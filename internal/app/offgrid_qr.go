package app

import (
	"github.com/ZahakJ/concord/internal/transport"
	"github.com/ZahakJ/concord/internal/transport/qr"
)

func init() {
	offgridFactories = append(offgridFactories, func(string) transport.Transport { return qr.Link{} })
}
