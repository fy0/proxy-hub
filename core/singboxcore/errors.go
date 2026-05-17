package singboxcore

import (
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
)

var (
	ErrCoreNotStarted      = errors.New("sing-box core is not started")
	ErrGroupNotFound       = errors.New("dynamic group not found")
	ErrGroupExists         = errors.New("dynamic group already exists")
	ErrNodeNotFound        = errors.New("dynamic node not found")
	ErrNodeExists          = errors.New("dynamic node already exists")
	ErrNoAvailableNode     = errors.New("no available node")
	ErrUnsupportedURI      = errors.New("unsupported proxy uri")
	ErrUnsupportedProtocol = errors.New("unsupported proxy protocol")
	ErrInvalidPort         = errors.New("invalid port")
	ErrOutboundPanic       = errors.New("sing-box outbound creation panicked")
)

type PortInUseError struct {
	Port int
	Err  error
}

func (err *PortInUseError) Error() string {
	if err == nil {
		return ""
	}
	if err.Port > 0 {
		return fmt.Sprintf("port %d is already in use", err.Port)
	}
	return "listen port is already in use"
}

func (err *PortInUseError) Unwrap() error {
	if err == nil {
		return nil
	}
	return err.Err
}

func NormalizeStartError(err error) error {
	if err == nil {
		return nil
	}
	if port := portFromListenError(err); port > 0 || isAddressInUseError(err) {
		return &PortInUseError{Port: port, Err: err}
	}
	return err
}

func isAddressInUseError(err error) bool {
	if err == nil {
		return false
	}
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		message := strings.ToLower(opErr.Err.Error())
		if strings.Contains(message, "address already in use") ||
			strings.Contains(message, "only one usage of each socket address") ||
			strings.Contains(message, "bind: an attempt was made to access a socket") {
			return true
		}
	}
	if errors.Is(err, os.ErrExist) {
		return true
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "address already in use") ||
		strings.Contains(message, "only one usage of each socket address") ||
		strings.Contains(message, "bind: an attempt was made to access a socket")
}

func portFromListenError(err error) int {
	if err == nil {
		return 0
	}
	message := err.Error()
	lastColon := strings.LastIndexByte(message, ':')
	if lastColon < 0 || lastColon == len(message)-1 {
		return 0
	}
	var port int
	for _, r := range message[lastColon+1:] {
		if r < '0' || r > '9' {
			break
		}
		port = port*10 + int(r-'0')
	}
	return port
}
