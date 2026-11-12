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
