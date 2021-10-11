package rootcerts

import (
	"bytes"
	"encoding/pem"
	"io"
)

// nolint
//go:generate ../bin/gencerts -download -package rootcerts -target rootcerts.go

func DERReader() (io.Reader, error) {
	buf := &bytes.Buffer{}
	for _, c := range Certs() {
		block := &pem.Block{
			Type:  "CERTIFICATE",
			Bytes: c.DER,
		}
		err := pem.Encode(buf, block)
		if err != nil {
			return nil, err
		}
	}
	return buf, nil
}
