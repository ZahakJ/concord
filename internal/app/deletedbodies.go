package app

// deletedBodiesClearedKey records that clearDeletedBodiesOnce has run to the
// end on this device.
const deletedBodiesClearedKey = "app.deleted_bodies_cleared.2"

// deletedBodiesClearedV1Key is the record of the first build's clear. That
// build ran its clear and then kept the body of every guild delete it
// received, on every device. So a device that has this record but not
// deletedBodiesClearedKey runs the clear once more, and keeps the bodies in
// the channels where it may reveal them.
const deletedBodiesClearedV1Key = "app.deleted_bodies_cleared"

// clearDeletedBodiesOnce erases, one time per device, the body that every
// deleted row already on disk still holds, on every device and in every
// channel, a moderator's included.
//
// The store does not record how a deleted row came to be deleted, by a delete
// applied here or by a tombstone from history sync, so it cannot keep the one
// and clear the other. Deletes applied after the run keep their bodies for
// RevealDeleted as before.
//
// The blobs the erased bodies named are released as a delete releases them
// (store.ReleaseErasedBlobs), which needs the blob index complete: the launch
// runs this after the index backfill.
//
// The run is recorded only when it finishes. One stopped by an error or by
// shutdown starts again at the next launch, and what it already cleared is
// not matched again; the blobs it noted are released by that run.
//
// On a device that already ran the first build's clear, the run keeps the
// bodies in the guilds where this device may reveal deleted messages: those
// deletes were made after that clear, and a moderator keeps them for reveal.
func (s *Service) clearDeletedBodiesOnce() { clearDeletedBodiesOnceIn(s.store, s.revealChannels) }

// bodyClearer is the part of the store the one-time clear uses.
type bodyClearer interface {
	GetSetting(key string) (string, error)
	SetSetting(key, value string) error
	ClearDeletedBodies(keepChannels []string) (int, error)
	ReleaseErasedBlobs() ([]string, error)
}

// clearDeletedBodiesOnceIn runs the clear on st; revealChannels lists the
// channels whose bodies a re-run keeps.
func clearDeletedBodiesOnceIn(st bodyClearer, revealChannels func() []string) {
	if v, err := st.GetSetting(deletedBodiesClearedKey); err != nil || v != "" {
		return
	}
	var keep []string
	if v, err := st.GetSetting(deletedBodiesClearedV1Key); err != nil {
		return
	} else if v != "" {
		keep = revealChannels()
	}
	if _, err := st.ClearDeletedBodies(keep); err != nil {
		return
	}
	if _, err := st.ReleaseErasedBlobs(); err != nil {
		return
	}
	_ = st.SetSetting(deletedBodiesClearedKey, "1")
}

// revealChannels lists the channels of the guilds where this device keeps
// deleted bodies for reveal (erasesDeletedBodies is false).
func (s *Service) revealChannels() []string {
	s.mu.RLock()
	ids := make([]string, 0, len(s.guilds))
	for id := range s.guilds {
		ids = append(ids, id)
	}
	s.mu.RUnlock()
	var out []string
	for _, id := range ids {
		if s.erasesDeletedBodies(id) {
			continue
		}
		s.mu.RLock()
		if g, ok := s.guilds[id]; ok {
			for _, c := range g.Channels {
				out = append(out, c.ID)
			}
		}
		s.mu.RUnlock()
	}
	return out
}
