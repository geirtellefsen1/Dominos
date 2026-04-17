// Package ca is the Dominion internal certificate authority. The same CA
// signs agent client certs (phase 5) and, when configured, the gateway's
// own TLS server cert.
package ca

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"net"
	"os"
	"time"

	"github.com/google/uuid"
)

type CA struct {
	Cert       *x509.Certificate
	CertPEM    []byte
	Key        ed25519.PrivateKey
	pool       *x509.CertPool
}

// ClientCAPool returns a CertPool containing the CA root, suitable for
// TLS client-cert verification.
func (c *CA) ClientCAPool() *x509.CertPool { return c.pool }

// Load reads DOMINION_CA_CERT_PEM and DOMINION_CA_KEY_PEM if set; otherwise
// generates a fresh self-signed CA (dev only — loud warning).
func Load() (*CA, error) {
	certPEM := os.Getenv("DOMINION_CA_CERT_PEM")
	keyPEM := os.Getenv("DOMINION_CA_KEY_PEM")
	if certPEM != "" && keyPEM != "" {
		return parse(certPEM, keyPEM)
	}
	slog.Warn("DOMINION_CA_CERT_PEM/DOMINION_CA_KEY_PEM not set; generating ephemeral CA (agents issued before a restart will stop authenticating)")
	return Generate("Dominion Internal CA")
}

// Generate makes a brand-new Ed25519 self-signed CA.
func Generate(cn string) (*CA, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: cn, Organization: []string{"Dominion"}},
		NotBefore:             time.Now().Add(-1 * time.Hour),
		NotAfter:              time.Now().AddDate(10, 0, 0),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, pub, priv)
	if err != nil {
		return nil, err
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, err
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	return build(cert, certPEM, priv)
}

func parse(certPEM, keyPEM string) (*CA, error) {
	cBlock, _ := pem.Decode([]byte(certPEM))
	if cBlock == nil || cBlock.Type != "CERTIFICATE" {
		return nil, errors.New("invalid DOMINION_CA_CERT_PEM")
	}
	cert, err := x509.ParseCertificate(cBlock.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse CA cert: %w", err)
	}
	kBlock, _ := pem.Decode([]byte(keyPEM))
	if kBlock == nil {
		return nil, errors.New("invalid DOMINION_CA_KEY_PEM")
	}
	key, err := x509.ParsePKCS8PrivateKey(kBlock.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse CA key: %w", err)
	}
	priv, ok := key.(ed25519.PrivateKey)
	if !ok {
		return nil, errors.New("CA key must be Ed25519")
	}
	return build(cert, []byte(certPEM), priv)
}

func build(cert *x509.Certificate, certPEM []byte, priv ed25519.PrivateKey) (*CA, error) {
	pool := x509.NewCertPool()
	pool.AddCert(cert)
	return &CA{Cert: cert, CertPEM: certPEM, Key: priv, pool: pool}, nil
}

// IssuedCert bundles what /admin/agents returns to a caller.
type IssuedCert struct {
	CertPEM    []byte
	KeyPEM     []byte
	Thumbprint string
}

// IssueAgent returns a new client cert + key for `agent:<id>`.
func (c *CA) IssueAgent(id uuid.UUID, displayName string) (*IssuedCert, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	serial, err := randomSerial()
	if err != nil {
		return nil, err
	}
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName:   "agent:" + id.String(),
			Organization: []string{"Dominion agents"},
			SerialNumber: id.String(),
		},
		NotBefore:   time.Now().Add(-5 * time.Minute),
		NotAfter:    time.Now().AddDate(1, 0, 0),
		KeyUsage:    x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	return c.signLeaf(tmpl, pub, priv)
}

// IssueServer returns a cert + key suitable for the gateway's TLS
// listener, valid for the given hostnames and IPs.
func (c *CA) IssueServer(hostnames []string, ips []net.IP) (*IssuedCert, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	serial, err := randomSerial()
	if err != nil {
		return nil, err
	}
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName:   "dominion-gateway",
			Organization: []string{"Dominion"},
		},
		NotBefore:   time.Now().Add(-5 * time.Minute),
		NotAfter:    time.Now().AddDate(1, 0, 0),
		KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:    append([]string{"localhost"}, hostnames...),
		IPAddresses: append([]net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")}, ips...),
	}
	return c.signLeaf(tmpl, pub, priv)
}

func (c *CA) signLeaf(tmpl *x509.Certificate, pub ed25519.PublicKey, priv ed25519.PrivateKey) (*IssuedCert, error) {
	der, err := x509.CreateCertificate(rand.Reader, tmpl, c.Cert, pub, c.Key)
	if err != nil {
		return nil, fmt.Errorf("sign leaf: %w", err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyDER, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return nil, err
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	return &IssuedCert{
		CertPEM:    certPEM,
		KeyPEM:     keyPEM,
		Thumbprint: Thumbprint(der),
	}, nil
}

// Thumbprint is the hex SHA-256 of a DER-encoded certificate.
func Thumbprint(der []byte) string {
	h := sha256.Sum256(der)
	return hex.EncodeToString(h[:])
}

func randomSerial() (*big.Int, error) {
	max := new(big.Int).Lsh(big.NewInt(1), 128)
	n, err := rand.Int(rand.Reader, max)
	if err != nil {
		return nil, err
	}
	return n, nil
}
