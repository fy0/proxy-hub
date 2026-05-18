package proxy

import "errors"

var (
	ErrNodeNotFound          = errors.New("proxy node not found")
	ErrMappingNotFound       = errors.New("port mapping not found")
	ErrUnsupportedURI        = errors.New("unsupported proxy uri")
	ErrUnsupportedProtocol   = errors.New("unsupported proxy protocol")
	ErrInvalidPort           = errors.New("invalid port")
	ErrInvalidAddress        = errors.New("invalid listen address")
	ErrNoAvailableNode       = errors.New("no available node")
	ErrListenPortTaken       = errors.New("listen port already exists")
	ErrUTLSRequired          = errors.New("reality requires a binary built with -tags with_utls")
	ErrSubscriptionNotFound  = errors.New("proxy subscription not found")
	ErrGroupNotFound         = errors.New("proxy group not found")
	ErrInvalidSubscription   = errors.New("invalid proxy subscription")
	ErrInvalidGroup          = errors.New("invalid proxy group")
	ErrInvalidHealthDuration = errors.New("invalid proxy health duration")
	ErrInvalidChain          = errors.New("invalid proxy chain")
	ErrInvalidSettingsBackup = errors.New("invalid proxy settings backup")
	ErrInvalidProbeURL       = errors.New("invalid proxy test url")
	ErrInvalidMappingSwitch  = errors.New("invalid mapping switch target")
)
