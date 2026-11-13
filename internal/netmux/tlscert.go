package netmux

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"
)

// LoadOrGenerateTLSConfig loads an existing X.509 certificate pair or generates
// a self-signed ECDSA P-256 certificate so HTTPS, WSS, and TURNS work out of the box on 1 port.
func LoadOrGenerateTLSConfig(certPath, keyPath string) (*tls.Config, error) {
	if certPath != "" && keyPath != "" {
		if _, errC := os.Stat(certPath); errC == nil {
			if _, errK := os.Stat(keyPath); errK == nil {
				cert, err := tls.LoadX509KeyPair(certPath, keyPath)
				if err == nil {
					return &tls.Config{
						Certificates: []tls.Certificate{cert},
						MinVersion:   tls.VersionTLS12,
					}, nil
				}
			}
		}
	}

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate ecdsa key: %w", err)
	}

	serialLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serialNumber, err := rand.Int(rand.Reader, serialLimit)
	if err != nil {
		return nil, fmt.Errorf("generate serial: %w", err)
	}

	tmpl := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{"GonDesk Single-Port Bridge"},
			CommonName:   "gondesk.local",
		},
		NotBefore:             time.Now().Add(-1 * time.Hour),
		NotAfter:              time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{"localhost", "gondesk.local"},
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1"), net.ParseIP("0.0.0.0")},
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &priv.PublicKey, priv)
	if err != nil {
		return nil, fmt.Errorf("create x509 cert: %w", err)
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: derBytes})
	privBytes, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		return nil, fmt.Errorf("marshal ec key: %w", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: privBytes})

	if certPath != "" && keyPath != "" {
		_ = os.MkdirAll(filepath.Dir(certPath), 0o755)
		_ = os.WriteFile(certPath, certPEM, 0o644)
		_ = os.WriteFile(keyPath, keyPEM, 0o600)
	}

	tlsCert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, fmt.Errorf("parse x509 keypair: %w", err)
	}

	return &tls.Config{
		Certificates: []tls.Certificate{tlsCert},
		MinVersion:   tls.VersionTLS12,
	}, nil
}

