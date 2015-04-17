package main

import (
	"math"

	"github.com/keybase/client/go/libkb"
	keybase_1 "github.com/keybase/client/protocol/go"
	"github.com/maxtaco/go-framed-msgpack-rpc/rpc2"
)

type BaseHandler struct {
	xp        *rpc2.Transport
	cli       *rpc2.Client
	loginCli  *keybase_1.LoginUiClient
	secretCli *keybase_1.SecretUiClient
	logCli    *keybase_1.LogUiClient
}

type LoginUI struct {
	sessionId int
	cli       *keybase_1.LoginUiClient
}

type SecretUI struct {
	sessionId int
	cli       *keybase_1.SecretUiClient
}

var sessionIDch chan int

func init() {
	sessionIDch = make(chan int)
	go func() {
		for {
			// wrap after MaxInt32 to be safe
			for i := 0; i < math.MaxInt32; i++ {
				sessionIDch <- i
			}
		}
	}()
}

func nextSessionID() int {
	return <-sessionIDch
}

func (h *BaseHandler) getRpcClient() *rpc2.Client {
	if h.cli == nil {
		h.cli = rpc2.NewClient(h.xp, libkb.UnwrapError)
	}
	return h.cli
}

func (h *BaseHandler) getLoginUICli() *keybase_1.LoginUiClient {
	if h.loginCli == nil {
		h.loginCli = &keybase_1.LoginUiClient{Cli: h.getRpcClient()}
	}
	return h.loginCli
}

func (h *BaseHandler) getLoginUI(sessionID int) libkb.LoginUI {
	return &LoginUI{sessionID, h.getLoginUICli()}
}

func (h *BaseHandler) getLocksmithUI(sessionID int) libkb.LocksmithUI {
	return NewRemoteLocksmithUI(sessionID, h.getRpcClient())
}

func (h *BaseHandler) getGPGUI(sessionID int) libkb.GPGUI {
	return NewRemoteGPGUI(sessionID, h.getRpcClient())
}

func (h *BaseHandler) getSecretUICli() *keybase_1.SecretUiClient {
	if h.secretCli == nil {
		h.secretCli = &keybase_1.SecretUiClient{Cli: h.getRpcClient()}
	}
	return h.secretCli
}

func (h *BaseHandler) getSecretUI(sessionId int) libkb.SecretUI {
	return &SecretUI{sessionId, h.getSecretUICli()}
}

func (h *BaseHandler) getLogUICli() *keybase_1.LogUiClient {
	if h.logCli == nil {
		h.logCli = &keybase_1.LogUiClient{Cli: h.getRpcClient()}
	}
	return h.logCli
}

func (h *BaseHandler) getLogUI(sessionId int) libkb.LogUI {
	return &LogUI{sessionId, h.getLogUICli()}
}

func (h *BaseHandler) getStreamUICli() *keybase_1.StreamUiClient {
	return &keybase_1.StreamUiClient{Cli: h.getRpcClient()}
}

func (h *BaseHandler) NewRemoteSelfIdentifyUI(sessionId int) *RemoteSelfIdentifyUI {
	c := h.getRpcClient()
	return &RemoteSelfIdentifyUI{RemoteBaseIdentifyUI{
		sessionId: sessionId,
		uicli:     keybase_1.IdentifyUiClient{Cli: c},
		logUI:     h.getLogUI(sessionId),
	}}
}

func (h *BaseHandler) NewRemoteIdentifyUI(sessionId int) *RemoteIdentifyUI {
	c := h.getRpcClient()
	return &RemoteIdentifyUI{RemoteBaseIdentifyUI{
		sessionId: sessionId,
		uicli:     keybase_1.IdentifyUiClient{Cli: c},
		logUI:     h.getLogUI(sessionId),
	}}
}
