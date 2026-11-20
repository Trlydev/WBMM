package manager

import "time"

// Troubleshooting: when the game crashed or mods failed, wbmm can switch
// mods off for a while and remembers which, so "bring them back" switches
// on exactly those.

// SetAside is a set of mods switched off for troubleshooting.
type SetAside struct {
	UIDs  []string  `json:"uids"`
	Names []string  `json:"names"`
	Since time.Time `json:"since"`
}

// SetAside returns the mods switched off for troubleshooting, if any.
func (m *Manager) SetAside() *SetAside {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.st.SetAside == nil || len(m.st.SetAside.UIDs) == 0 {
		return nil
	}
	sa := *m.st.SetAside
	return &sa
}

// PutAside switches mods off and remembers them (added to those already
// set aside).
func (m *Manager) PutAside(mods []Mod) error {
	if gameRunning() {
		return ErrGameRunning
	}
	var done []Mod
	for _, md := range mods {
		if !md.Enabled || md.UID == "" {
			continue
		}
		if err := m.SetEnabled(md.UID, false); err != nil {
			return err
		}
		done = append(done, md)
	}
	return m.update(func(st *state) {
		if st.SetAside == nil {
			st.SetAside = &SetAside{Since: time.Now()}
		}
		have := map[string]bool{}
		for _, u := range st.SetAside.UIDs {
			have[u] = true
		}
		for _, md := range done {
			if !have[md.UID] {
				st.SetAside.UIDs = append(st.SetAside.UIDs, md.UID)
				st.SetAside.Names = append(st.SetAside.Names, md.Name)
			}
		}
	})
}

// BringBack switches the set-aside mods on again (through the safety check
// and dependency checks, as any switch) and forgets the set. It returns
// the names of those it couldn't switch on.
func (m *Manager) BringBack() (failed []string, err error) {
	if gameRunning() {
		return nil, ErrGameRunning
	}
	sa := m.SetAside()
	if sa == nil {
		return nil, nil
	}
	for i, uid := range sa.UIDs {
		if err := m.SetEnabled(uid, true); err != nil {
			failed = append(failed, sa.Names[i])
		}
	}
	return failed, m.update(func(st *state) { st.SetAside = nil })
}

// SeenRun reports whether the person dismissed what the game run at t
// reported.
func (m *Manager) SeenRun(t time.Time) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return !m.st.SeenRun.Before(t)
}

// DismissRun hides what the game run at t reported.
func (m *Manager) DismissRun(t time.Time) error {
	return m.update(func(st *state) { st.SeenRun = t })
}
