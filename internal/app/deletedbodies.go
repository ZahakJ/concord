package app

// deletedBodiesClearedKey records that clearDeletedBodiesOnce has run to the
// end on this device.
const deletedBodiesClearedKey = "app.deleted_bodies_cleared"

// clearDeletedBodiesOnce erases, one time per device, the body that every
// deleted row already on disk still holds, on every device and in every
// channel, a moderator's included.
//
// The store does not record how a deleted row came to be deleted, by a delete
// applied here or by a tombstone from history sync, so it cannot keep the one
// and clear the other. Deletes applied after the run keep their bodies for
// RevealDeleted as before.
//
// The run is recorded only when it finishes. One stopped by an error or by
// shutdown starts again at the next launch, and what it already cleared is
// not matched again.
func (s *Service) clearDeletedBodiesOnce() { clearDeletedBodiesOnceIn(s.store) }

// bodyClearer is the part of the store the one-time clear uses.
type bodyClearer interface {
	GetSetting(key string) (string, error)
	SetSetting(key, value string) error
	ClearDeletedBodies(keepChannels []string) (int, error)
}

func clearDeletedBodiesOnceIn(st bodyClearer) {
	if v, err := st.GetSetting(deletedBodiesClearedKey); err != nil || v != "" {
		return
	}
	if _, err := st.ClearDeletedBodies(nil); err != nil {
		return
	}
	_ = st.SetSetting(deletedBodiesClearedKey, "1")
}
