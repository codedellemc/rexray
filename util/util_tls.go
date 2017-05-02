package util

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	log "github.com/Sirupsen/logrus"
	"github.com/akutz/gotil"
	apitypes "github.com/codedellemc/libstorage/api/types"
)

var (
	orgName       = "libstorage"
	certBlockType = "CERTIFICATE"
	keyBlockType  = "RSA PRIVATE KEY"
)

// CreateSelfCert creates self-signed certificate and a private key
func CreateSelfCert(ctx apitypes.Context, certPath, keyPath, host string) error {

	// if files exist, ignore
	_, cerErr := os.Stat(certPath)
	_, keyErr := os.Stat(keyPath)
	if cerErr == nil && keyErr == nil {
		ctx.WithFields(log.Fields{
			"host":     host,
			"certPath": certPath,
			"certKey":  certPath,
		}).Debug("skipping self-cert creation, files exist")
		return nil
	}

	certRoot := filepath.Dir(certPath)
	keyRoot := filepath.Dir(keyPath)
	if err := os.MkdirAll(certRoot, 0755); err != nil {
		ctx.WithFields(log.Fields{
			"host":     host,
			"certRoot": certRoot,
		}).Debug("created dir")

		return err
	}
	if keyRoot != certRoot {
		if err := os.MkdirAll(keyRoot, 0755); err != nil {
			ctx.WithFields(log.Fields{
				"host":    host,
				"keyRoot": keyRoot,
			}).Debug("created dir")
			return err
		}
	}

	serialNumberLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serialNumber, err := rand.Int(rand.Reader, serialNumberLimit)
	if err != nil {
		return err
	}

	tmpl := x509.Certificate{
		SerialNumber: serialNumber,

		Subject: pkix.Name{
			Organization: []string{orgName},
			CommonName:   host,
		},

		IPAddresses: []net.IP{net.ParseIP(host)},
		DNSNames:    []string{"localhost"},

		NotBefore:          time.Now(),
		NotAfter:           time.Now().AddDate(1, 0, 0),
		SignatureAlgorithm: x509.SHA256WithRSA,

		KeyUsage: x509.KeyUsageKeyEncipherment |
			x509.KeyUsageDigitalSignature,

		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}

	privKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return err
	}

	// gen cert file
	ctx.WithField("certFile", certPath).Debug("creating cert file")
	certBlock, err := x509.CreateCertificate(
		rand.Reader,
		&tmpl,
		&tmpl,
		&privKey.PublicKey,
		privKey)
	if err != nil {
		return err
	}

	certFile, err := os.Create(certPath)
	if err != nil {
		return err
	}
	defer certFile.Close()
	if err := pem.Encode(
		certFile,
		&pem.Block{Type: certBlockType, Bytes: certBlock}); err != nil {
		return err
	}

	// gen key file
	ctx.WithField("keyFile", keyPath).Debug("creating key file")
	keyFile, err := os.OpenFile(keyPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	defer keyFile.Close()

	keyBlock := x509.MarshalPKCS1PrivateKey(privKey)
	if err != nil {
		return err
	}

	if err := pem.Encode(
		keyFile,
		&pem.Block{Type: keyBlockType, Bytes: keyBlock}); err != nil {
		return err
	}

	ctx.WithFields(log.Fields{
		"certPath": certPath,
		"certKey":  certPath,
	}).Debug("self-cert files created")

	return nil
}

// AssertTrustedHost presents user with prompt to accept/reject host as trusted
func AssertTrustedHost(
	ctx apitypes.Context,
	host,
	algo string,
	fingerprint []byte,
) bool {
	trusted := "no"
	fmt.Printf("\nRejecting connection to unknown host %s.\n", host)
	fmt.Printf("SHA Fingerprint presented: %s:%x/%x.\n",
		algo, fingerprint[0:8], fingerprint[len(fingerprint)-2:])
	fmt.Print("Do you want to save host to known_hosts file? (yes/no): ")
	fmt.Scanf("%s", &trusted)
	if strings.EqualFold(trusted, "yes") {
		return true
	}
	return false
}

// AddKnownHost adds unknown host to know_hosts file
func AddKnownHost(
	ctx apitypes.Context,
	knownHostPath,
	host, algo string,
	fingerprint []byte) error {

	knownHostPathDir := filepath.Dir(knownHostPath)

	if !gotil.FileExists(knownHostPathDir) {
		if err := os.MkdirAll(knownHostPathDir, 0755); err != nil {
			return err
		}
		ctx.WithField("dir", knownHostPathDir).
			Debug("created directory for known_hosts")
	}

	khFile, err := os.OpenFile(
		knownHostPath, os.O_WRONLY|
			os.O_CREATE|
			os.O_APPEND, 0600)
	if err != nil {
		return err
	}
	defer khFile.Close()

	fmt.Fprintf(khFile, "%s %s %x\n", host, algo, fingerprint)
	if host == "127.0.0.1" {
		fmt.Fprintf(khFile, "%s %s %x\n", "localhost", algo, fingerprint)
	}

	ctx.WithFields(log.Fields{
		"host":        host,
		"algo":        algo,
		"fingerprint": fmt.Sprintf("%x", fingerprint),
	}).Debug("fingerprint added to known_hosts file")

	return nil
}
