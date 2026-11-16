package app

import "time"

// releaseBlobs lets go of the attachment blobs a message held, after its
// body was erased on this device: by a delete for everyone where this device
// keeps no deleted bodies, by its disappearing timer, or by Empty trash. A
// blob nothing else keeps is removed and banned (store.ReleaseBlobs).
func (s *Service) releaseBlobs(messageID string) {
	_, _ = s.store.ReleaseBlobs(messageID)
}

// runBlobRefsBackfill indexes the blobs of rows stored before the index
// existed, one bounded batch at a time, until it is complete. Until then no
// blob is removed or banned (store.ReleaseBlobs).
func (s *Service) runBlobRefsBackfill() {
	for {
		done, err := s.store.BackfillBlobRefs()
		if done {
			return
		}
		pause := 10 * time.Millisecond // give the connection back between batches
		if err != nil {
			pause = time.Minute
		}
		select {
		case <-s.ctx.Done():
			return
		case <-time.After(pause):
		}
	}
}
