package webui

import "embed"

// Static contains the disposable mobile web UI assets.
//
//go:embed static/*
var Static embed.FS
