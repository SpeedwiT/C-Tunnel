package tunnel

import "github.com/SpeedwiT/c-tunnel/internal/config"

func decodePSK(psk string) ([]byte, error) { return config.DecodePSK(psk) }
