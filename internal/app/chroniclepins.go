package app

import "github.com/ZahakJ/concord/internal/store"

// Pinning archive pictures on the devices that read them.
//
// A peer is served only a blob something on this device keeps (BlobServable):
// a held message that names it, the guild GIF pack, or a pin. An archive's
// pictures are named by no held message, only by the archive's pages, and the
// importing device pins them. A member who reads the archive pins the
// pictures it holds for the pages it reads, so it can pass them on while the
// importer is offline. The pins are bounded per guild and per device
// (store.ChroniclePinsPerGuild and the byte bounds next to it); past a bound
// a picture is still shown, just not pinned, so not passed on from here.
//
// A pin is taken back when the guild's archive is replaced by another, when
// the guild is left, and, with the picture removed and banned, when the
// archive is removed (releaseChronicleBlobs).

// chronicleName is the guild archive a page picture was named by.
type chronicleName struct {
	guild, chronicle string
}

// maxChronNamed bounds the remembered names. A page names tens of pictures;
// the map only has to span the moment between a page being read and its
// pictures being fetched. A picture whose name was forgotten is pinned when
// its page is read again, since it is held by then.
const maxChronNamed = 4096

// noteChroniclePictures records that guildID's archive chronicleID names the
// pictures of msgs, and pins those this device already holds.
func (s *Service) noteChroniclePictures(guildID, chronicleID string, msgs []chronicleMessage) {
	var ids []string
	for _, m := range msgs {
		for _, tok := range m.Attach {
			ids = append(ids, store.BlobIDsIn(tok)...)
		}
	}
	if len(ids) == 0 {
		return
	}
	s.chronNamedMu.Lock()
	if s.chronNamed == nil || len(s.chronNamed)+len(ids) > maxChronNamed {
		s.chronNamed = make(map[string]chronicleName, len(ids))
	}
	for _, id := range ids {
		s.chronNamed[id] = chronicleName{guild: guildID, chronicle: chronicleID}
	}
	s.chronNamedMu.Unlock()
	for _, id := range ids {
		_, _ = s.store.PinChronicleBlob(guildID, chronicleID, id)
	}
}

// pinFetchedChroniclePicture pins a blob just fetched from a peer if an
// archive page read here named it. Past the bounds nothing is pinned and
// nothing fails.
func (s *Service) pinFetchedChroniclePicture(blobID string) {
	s.chronNamedMu.Lock()
	n, ok := s.chronNamed[blobID]
	s.chronNamedMu.Unlock()
	if !ok {
		return
	}
	_, _ = s.store.PinChronicleBlob(n.guild, n.chronicle, blobID)
}

// releaseChronicleBlobs is what removing a guild's archive does to its
// pictures on this device: the pins made for it are taken back, and each
// picture nothing else keeps is removed and banned, so this device neither
// serves nor fetches it again.
func (s *Service) releaseChronicleBlobs(guildID, chronicleID string) {
	_, _ = s.store.ReleaseChronicleBlobs(guildID, chronicleID)
	s.chronNamedMu.Lock()
	for id, n := range s.chronNamed {
		if n.guild == guildID && n.chronicle == chronicleID {
			delete(s.chronNamed, id)
		}
	}
	s.chronNamedMu.Unlock()
}

// unpinReplacedChronicles takes back the pins of guildID's archives other
// than the one it shows now.
func (s *Service) unpinReplacedChronicles(guildID string) {
	_, id, ok := s.guildChronicle(guildID)
	if !ok {
		return
	}
	_ = s.store.UnpinOtherChronicles(guildID, id)
}
