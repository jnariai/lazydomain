package proxy

import (
	"bytes"
	"context"
	"crypto/sha1"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"strings"

	"github.com/jnariai/lazydomain/internal/docker"
)

type RootCA struct {
	PEM        []byte
	Cert       *x509.Certificate
	Thumbprint string // SHA-1 of the DER bytes, uppercase hex (the Windows thumbprint)
}

// IntermediateTooShort reports whether the intermediate predates the
// IntermediateLifetime setting (Caddy's default is 7 days). Caddy only applies
// a new intermediate_lifetime when it next renews the intermediate, and until
// then every leaf is clamped to the old intermediate's expiry.
func IntermediateTooShort(cert *x509.Certificate) bool {
	return cert.NotAfter.Sub(cert.NotBefore) < LeafLifetime
}

func loadCert(ctx context.Context, path string) (*x509.Certificate, error) {
	raw, err := docker.ReadFile(ctx, ContainerName, path)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(raw)
	if block == nil || block.Type != "CERTIFICATE" {
		return nil, fmt.Errorf("%s is not a PEM certificate", path)
	}
	return x509.ParseCertificate(block.Bytes)
}

// regenerateIntermediate makes Caddy issue a new intermediate (and new leaves
// from it) under the existing root, so nothing has to be trusted again. It
// relies on the current config already being loaded: --resume brings it back
// after the restart.
func regenerateIntermediate(ctx context.Context) error {
	if err := docker.Exec(ctx, ContainerName, "rm", "-rf",
		intermediateCertPath, intermediateKeyPath, leafCertsDir); err != nil {
		return fmt.Errorf("remove old certificates: %w", err)
	}
	if err := docker.Restart(ctx, ContainerName); err != nil {
		return fmt.Errorf("restart %s: %w", ContainerName, err)
	}
	return nil
}

func LoadRootCA(ctx context.Context) (*RootCA, error) {
	raw, err := docker.ReadFile(ctx, ContainerName, RootCertPath)
	if err != nil {
		return nil, fmt.Errorf("read root CA from %s: %w", ContainerName, err)
	}
	block, _ := pem.Decode(raw)
	if block == nil || block.Type != "CERTIFICATE" {
		return nil, errors.New("root.crt is not a PEM certificate")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse root.crt: %w", err)
	}
	sum := sha1.Sum(block.Bytes)
	return &RootCA{
		PEM:        append(bytes.TrimSpace(raw), '\n'),
		Cert:       cert,
		Thumbprint: strings.ToUpper(hex.EncodeToString(sum[:])),
	}, nil
}
