package cloudenv

import "fmt"

type Provider int

const (
	ProviderUnknown Provider = iota
	ProviderEC2
	ProviderGCE
)

func (p Provider) String() string {
	strings := [...]string{"UNKNOWN", "EC2", "GCE"}

	// prevent panicking in case of status is out-of-range
	if p < ProviderUnknown || p > ProviderGCE {
		return strings[0]
	}

	return strings[p]
}

func (p *Provider) UnmarshalText(text []byte) error {
	s := string(text)

	provider, ok := map[string]Provider{
		"EC2": ProviderEC2,
		"GCE": ProviderGCE,
	}[s]
	if !ok {
		return fmt.Errorf("unknown provider: %q", s)
	}

	*p = provider
	return nil
}
