package proxy

import "errors"

var (
	ErrNodeNotFound        = errors.New("proxy node not found")
	ErrMappingNotFound     = errors.New("port mapping not found")
	ErrUnsupportedURI      = errors.New("unsupported proxy uri")
	ErrUnsupportedProtocol = errors.New("unsupported proxy protocol")
	ErrInvalidPort         = errors.New("invalid port")
	ErrInvalidAddress      = errors.New("invalid listen address")
	ErrNoAvailableNode     = errors.New("no available node")
	ErrListenPortTaken     = errors.New("listen port already exists")
)
