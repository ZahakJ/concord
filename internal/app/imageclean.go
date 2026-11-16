package app

import (
	"encoding/base64"
	"fmt"
	"regexp"

	"github.com/ZahakJ/concord/internal/imgmeta"
)

// Every picture this device sends is cleaned of its metadata in the core,
// right before it is sealed or stored, so the desktop, the web build and the
// phone share one implementation, and a page path that forgets to clean
// (paste, drop, the share sheet, a picker) cannot leak anything. The chat
// paths meet in sealAttachment and SendFile; the pictures that travel as data
// URIs (avatars, banners, guild icons, forum art, stories, custom emoji) go
// through cleanImageURI in their setters. See package imgmeta for what is
// kept and what is removed.

// cleanImage strips metadata from picture bytes about to leave this device.
// Bytes in no picture format the stripper knows are returned unchanged.
func cleanImage(plain []byte) ([]byte, error) {
	out, err := imgmeta.Strip(plain)
	if err != nil {
		return nil, fmt.Errorf("app: couldn't remove the details stored inside this picture (%v); send it as a png, jpeg, gif or webp", err)
	}
	return out, nil
}

var imageURIRe = regexp.MustCompile(`^data:image/(png|jpeg|gif|webp);base64,([A-Za-z0-9+/=]+)$`)

// cleanImageURI is cleanImage for a picture held as a data URI. Anything that
// is not a base64 raster URI ("", a "preset:<id>", a malformed value the
// caller's own validation refuses) comes back as it is, and so does a picture
// that had nothing to remove, byte for byte.
func cleanImageURI(uri string) (string, error) {
	m := imageURIRe.FindStringSubmatch(uri)
	if m == nil {
		return uri, nil
	}
	plain, err := base64.StdEncoding.DecodeString(m[2])
	if err != nil {
		return uri, nil
	}
	out, err := cleanImage(plain)
	if err != nil {
		return "", err
	}
	if len(out) == len(plain) && string(out) == string(plain) {
		return uri, nil
	}
	return "data:image/" + m[1] + ";base64," + base64.StdEncoding.EncodeToString(out), nil
}
