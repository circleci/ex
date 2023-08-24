package rootcerts

import (
	"crypto/x509"
	"encoding/pem"
	"fmt"
)

// AddPEM Adds a the supplied PEM formatted Certificate to the set of certs used by Rootcerts.
// This is useful when an individual CA needs to be added to the trust chain.
//
// AddPEM only support Certificates, it doesn't support Private keys.
func AddPEM(pemCert []byte) error {
	raw := pemCert

	var certList []*x509.Certificate
	for {
		block, rest := pem.Decode(raw)
		if block == nil {
			break
		}
		if block.Type == "CERTIFICATE" {
			x509Certs, err := x509.ParseCertificates(block.Bytes)
			if err != nil {
				return err
			}
			certList = append(certList, x509Certs...)
		} else {
			// We shouldn't need to support PrivateKeys right now. It isn't worth blocking importing all certificate
			// when a single PrivateKey being included in the bundle.
			// If we return an error here, no certs will be imported which is a worse situation.
			fmt.Println("Private Key found in CA Bundle, PrivateKeys are currently not supported so it will be ignored")
		}
		raw = rest
	}

	for _, v := range certList {
		certs = append(certs, (Cert{
			Label:  v.Issuer.CommonName,
			Serial: v.SerialNumber.String(),
			Trust:  1,
			DER:    v.Raw,
		}))
	}
	return nil
}
