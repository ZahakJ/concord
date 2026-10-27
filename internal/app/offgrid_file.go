package app

import "github.com/ZahakJ/concord/internal/transport"

func init() {
	offgridFactories = append(offgridFactories, func(string) transport.Transport { return transport.FileLink{} })
}
