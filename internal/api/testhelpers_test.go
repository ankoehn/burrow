package api

import (
	"io"
	"log/slog"
	"net/http/cookiejar"
)

func discardLog() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func cookiejarNew() (*cookiejar.Jar, error) { return cookiejar.New(nil) }
