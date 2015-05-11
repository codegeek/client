package libkb

import (
	"fmt"
	"sync"

	triplesec "github.com/keybase/go-triplesec"
)

type Account struct {
	Contextified
	secretSyncerMu sync.RWMutex
	secretSyncer   *SecretSyncer
	sync.RWMutex   // protects the rest:
	localSession   *Session
	loginSession   *LoginSession
	streamCache    *StreamCache
	skbKeyring     *SKBKeyringFile
}

func NewAccount(g *GlobalContext) *Account {
	return &Account{
		localSession: newSession(g),
		secretSyncer: NewSecretSyncer(g),
		Contextified: NewContextified(g),
	}
}

func (a *Account) LocalSession() *Session {
	a.RLock()
	defer a.RUnlock()
	return a.localSession
}

func (a *Account) UnloadLocalSession() {
	a.Lock()
	a.localSession = newSession(a.G())
	a.Unlock()
}

// LoggedIn returns true if the user is logged in.  It does not
// try to load the session.
func (a *Account) LoggedIn() bool {
	return a.LocalSession().IsLoggedIn()
}

// LoggedInLoad will load and check the session with the api server if necessary.
func (a *Account) LoggedInLoad() (bool, error) {
	return a.LocalSession().loadAndCheck()
}

func (a *Account) LoadLoginSession(emailOrUsername string) error {
	if a.LoginSession().ExistsFor(emailOrUsername) {
		return nil
	}

	ls := NewLoginSession(emailOrUsername, a.G())
	if err := ls.Load(); err != nil {
		return err
	}
	a.setLoginSession(ls)
	return nil
}

func (a *Account) CreateLoginSessionWithSalt(emailOrUsername string, salt []byte) error {
	if a.loginSessionExists() {
		return fmt.Errorf("CreateLoginSessionWithSalt called, but Account already has LoginSession")
	}

	ls := NewLoginSessionWithSalt(emailOrUsername, salt, a.G())
	a.setLoginSession(ls)
	return nil
}

func (a *Account) setLoginSession(ls *LoginSession) {
	a.Lock()
	defer a.Unlock()
	if a.loginSession != nil {
		// this usually happens in tests that don't call G.Logout() to logout.
		// But it probably signifies an error.
		a.G().Log.Warning("Account: overwriting loginSession")
	}

	a.loginSession = ls
}

func (a *Account) loginSessionExists() bool {
	return a.LoginSession() != nil
}

func (a *Account) LoginSession() *LoginSession {
	a.RLock()
	defer a.RUnlock()
	return a.loginSession
}

func (a *Account) Logout() error {
	a.ClearStreamCache()

	a.RLock()
	if err := a.localSession.Logout(); err != nil {
		a.RUnlock()
		return err
	}
	a.RUnlock()

	a.UnloadLocalSession()
	a.Lock()
	a.loginSession = nil
	a.skbKeyring = nil
	a.Unlock()

	a.secretSyncerMu.Lock()
	a.secretSyncer.Clear()
	a.secretSyncer = NewSecretSyncer(a.G())
	a.secretSyncerMu.Unlock()

	return nil
}

func (a *Account) CreateStreamCache(tsec *triplesec.Cipher, pps PassphraseStream) {
	a.Lock()
	defer a.Unlock()
	if a.streamCache != nil {
		a.G().Log.Warning("Account.CreateStreamCache overwriting exisitng StreamCache")
	}
	a.streamCache = NewStreamCache(tsec, pps)
}

func (a *Account) CreateStreamCacheViaStretch(passphrase string) error {
	a.Lock()
	defer a.Unlock()

	if a.streamCache.Valid() {
		return nil
	}

	salt, err := a.loginSession.Salt()
	if err != nil {
		return err
	}

	tsec, pps, err := StretchPassphrase(passphrase, salt)
	if err != nil {
		return err
	}

	a.streamCache = NewStreamCache(tsec, pps)

	return nil
}

func (a *Account) StreamCache() *StreamCache {
	a.RLock()
	defer a.RUnlock()
	return a.streamCache
}

func (a *Account) ClearStreamCache() {
	a.Lock()
	defer a.Unlock()
	a.streamCache.Clear()
	a.streamCache = nil
}

func (a *Account) SecretSyncer() *SecretSyncer {
	a.secretSyncerMu.RLock()
	defer a.secretSyncerMu.RUnlock()
	return a.secretSyncer
}

func (a *Account) RunSecretSyncer(uid *UID) error {
	return RunSyncer(a.SecretSyncer(), uid, a.LoggedIn(), a.localSession)
}

func (a *Account) Keyring() (*SKBKeyringFile, error) {
	if a.localSession == nil {
		a.G().Log.Warning("local session is nil")
	}
	a.LocalSession().loadAndCheck()
	if a.localSession == nil {
		a.G().Log.Warning("local session after load is nil")
	}
	a.RLock()
	kr := a.skbKeyring
	a.RUnlock()
	if kr != nil {
		return kr, nil
	}

	a.Lock()
	defer a.Unlock()
	unp := a.localSession.GetUsername()
	// not sure how this could happen, but just in case:
	if unp == nil {
		return nil, NoUsernameError{}
	}
	kr, err := LoadSKBKeyring(*unp, a.G())
	if err != nil {
		return nil, err
	}
	a.skbKeyring = kr
	return a.skbKeyring, nil
}

// LockedLocalSecretKey looks in the local keyring to find a key
// for the given user.  Returns non-nil if one was found, and nil
// otherwise.
func (a *Account) LockedLocalSecretKey(ska SecretKeyArg) *SKB {
	var ret *SKB
	me := ska.Me
	a.EnsureUsername(me.GetName())

	keyring, err := a.Keyring()
	if err != nil || keyring == nil {
		var s string
		if err != nil {
			s = " (" + err.Error() + ")"
		}
		a.G().Log.Debug("| No secret keyring found" + s)
		return nil
	}

	ckf := me.GetComputedKeyFamily()
	if ckf == nil {
		a.G().Log.Warning("No ComputedKeyFamily found for %s", me.name)
		return nil
	}

	if !ska.UseDeviceKey() {
		a.G().Log.Debug("| not using device key; preferences have disabled it")
	} else if did := a.G().Env.GetDeviceID(); did == nil {
		a.G().Log.Debug("| Could not get device id")
	} else if key, err := ckf.GetSibkeyForDevice(*did); err != nil {
		a.G().Log.Debug("| No key for current device: %s", err.Error())
	} else if key == nil {
		a.G().Log.Debug("| Key for current device is nil")
	} else {
		kid := key.GetKid()
		a.G().Log.Debug("| Found KID for current device: %s", kid)
		ret = keyring.LookupByKid(kid)
		if ret != nil {
			a.G().Log.Debug("| Using device key: %s", kid)
		}
	}

	if ret == nil && ska.SearchForKey() {
		a.G().Log.Debug("| Looking up secret key in local keychain")
		ret = keyring.SearchWithComputedKeyFamily(ckf, ska)
	}
	return ret
}

func (a *Account) Shutdown() error {
	return a.LocalSession().Write()
}

func (a *Account) EnsureUsername(username string) {
	su := a.LocalSession().GetUsername()
	if su == nil {
		a.LocalSession().SetUsername(username)
		return
	}
	if *su != username {
		a.Logout()
		a.LocalSession().SetUsername(username)
	}

}

// XXX not sure this is the best place for this...
// XXX put it through loginstate external func?
func (a *Account) UserInfo() (uid UID, username, token string, deviceSubkeyKid KID, err error) {
	if !a.LoggedIn() {
		err = LoginRequiredError{}
		return
	}

	user, err := LoadMe(LoadUserArg{})
	if err != nil {
		return
	}

	// lock everything to make sure the values refer to same user
	a.RLock()
	defer a.RUnlock()
	deviceSubkeyKid, err = user.GetDeviceSubkeyKid(a.G())
	if err != nil {
		deviceSubkeyKid = KID{}
		return
	}

	uid = user.GetUid()
	username = user.GetName()
	token = a.localSession.GetToken()
	return
}
