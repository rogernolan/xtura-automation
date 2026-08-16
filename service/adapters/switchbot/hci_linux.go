//go:build linux

package switchbot

import (
	"context"
	"encoding/binary"
	"fmt"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

const (
	hciChannelUser = unix.HCI_CHANNEL_USER
	ogfLETCommands = 0x08

	ocfLESetScanParams = 0x000b
	ocfLESetScanEnable = 0x000c

	hciEventCommandComplete = 0x0e
	hciEventLEMeta          = 0x3e
	leSubeventAdvReport     = 0x02
)

const (
	scanPassive   = 0x00
	scanInterval  = 0x0060 // 60 x 0.625ms = 37.5ms
	scanWindow    = 0x0030 // 30 x 0.625ms = 18.75ms
	ownAddrPublic = 0x00
	scanAll       = 0x00
)

const commandTimeout = 3 * time.Second

// scanLoop performs one continuous passive scan session, feeding HCI events
// to a.handleEvent. It returns an error to trigger the reconnect backoff.
func (a *Adapter) scanLoop(ctx context.Context, device string) error {
	devID, err := hciDevID(device)
	if err != nil {
		return err
	}
	fd, err := unix.Socket(unix.AF_BLUETOOTH, unix.SOCK_RAW|unix.SOCK_CLOEXEC, unix.BTPROTO_HCI)
	if err != nil {
		return fmt.Errorf("open HCI socket: %w", err)
	}
	defer unix.Close(fd)

	if err := unix.Bind(fd, &unix.SockaddrHCI{Dev: uint16(devID), Channel: hciChannelUser}); err != nil {
		return fmt.Errorf("bind HCI user channel: %w", err)
	}

	// Closing the socket from another goroutine unblocks a pending Read so
	// context cancellation ends the scan promptly.
	fdClosed := make(chan struct{})
	defer close(fdClosed)
	go func() {
		select {
		case <-ctx.Done():
			_ = unix.Close(fd)
		case <-fdClosed:
		}
	}()

	scanParams := []byte{
		scanPassive,
		byte(scanInterval & 0xff), byte(scanInterval >> 8),
		byte(scanWindow & 0xff), byte(scanWindow >> 8),
		ownAddrPublic,
		scanAll,
	}
	if err := hciCommand(fd, opcode(ogfLETCommands, ocfLESetScanParams), scanParams); err != nil {
		return err
	}
	if err := hciCommand(fd, opcode(ogfLETCommands, ocfLESetScanEnable), []byte{0x01, 0x00}); err != nil {
		return err
	}

	a.logger.Printf("switchbot scan active: hci%d", devID)
	buffer := make([]byte, 4096)
	for {
		n, err := unix.Read(fd, buffer)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("read HCI events: %w", err)
		}
		if n == 0 {
			continue
		}
		a.handleEvent(buffer[:n])
	}
}

func hciDevID(device string) (int, error) {
	if !strings.HasPrefix(device, "hci") {
		return 0, fmt.Errorf("invalid HCI device %q", device)
	}
	id, err := strconv.Atoi(strings.TrimPrefix(device, "hci"))
	if err != nil || id < 0 {
		return 0, fmt.Errorf("invalid HCI device %q", device)
	}
	return id, nil
}

func opcode(ogf, ocf byte) uint16 {
	return uint16(ogf)<<10 | uint16(ocf)
}

// hciCommand sends a command and waits for its command-complete event,
// surfacing a non-zero status as an error.
func hciCommand(fd int, opcode uint16, params []byte) error {
	buf := make([]byte, 3+len(params))
	binary.LittleEndian.PutUint16(buf[0:2], opcode)
	buf[2] = byte(len(params))
	copy(buf[3:], params)
	if _, err := unix.Write(fd, buf); err != nil {
		return fmt.Errorf("write HCI command 0x%04x: %w", opcode, err)
	}
	return waitCommandComplete(fd, opcode, commandTimeout)
}

func waitCommandComplete(fd int, opcode uint16, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			break
		}
		pfds := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
		if _, err := unix.Poll(pfds, int(remaining.Milliseconds())); err != nil {
			return fmt.Errorf("poll HCI: %w", err)
		}
		if pfds[0].Revents&unix.POLLIN == 0 {
			continue
		}
		event := make([]byte, 4096)
		n, err := unix.Read(fd, event)
		if err != nil {
			return fmt.Errorf("read HCI command complete: %w", err)
		}
		if n < 6 || event[0] != hciEventCommandComplete {
			continue
		}
		if binary.LittleEndian.Uint16(event[3:5]) != opcode {
			continue
		}
		if status := event[5]; status != 0 {
			return fmt.Errorf("HCI command 0x%04x failed: status 0x%02x", opcode, status)
		}
		return nil
	}
	return fmt.Errorf("timed out waiting for HCI command 0x%04x", opcode)
}
