package main

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	mr "math/rand"
	"os"
	"time"
)

const (
	tlsRSABits           = 3072
	tlsDefaultCommonName = "syncthing"
)

func getTlsConfig() (*tls.Config, tls.Certificate) {
	cert, err := tls.LoadX509KeyPair(locations[locCertFile], locations[locKeyFile])
	if err != nil {
		cert, err = newCertificate(locations[locCertFile], locations[locKeyFile], tlsDefaultCommonName)
		if err != nil {
			l.Fatalln("load cert:", err)
		}
	}

	// The TLS configuration is used for both the listening socket and outgoing
	// connections.
	tlsCfg := &tls.Config{
		Certificates:           []tls.Certificate{cert},
		NextProtos:             []string{bepProtocolName},
		ClientAuth:             tls.RequestClientCert,
		SessionTicketsDisabled: true,
		InsecureSkipVerify:     true,
		MinVersion:             tls.VersionTLS12,
		CipherSuites: []uint16{
			tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA,
			tls.TLS_ECDHE_ECDSA_WITH_AES_128_CBC_SHA,
			tls.TLS_ECDHE_RSA_WITH_AES_256_CBC_SHA,
			tls.TLS_ECDHE_ECDSA_WITH_AES_256_CBC_SHA,
		},
	}

	return tlsCfg, cert
}

func newCertificate(certFile, keyFile, name string) (tls.Certificate, error) {
	l.Infof("Generating RSA key and certificate for %s...", name)

	priv, err := rsa.GenerateKey(rand.Reader, tlsRSABits)
	if err != nil {
		l.Fatalln("generate key:", err)
	}

	notBefore := time.Now()
	notAfter := time.Date(2049, 12, 31, 23, 59, 59, 0, time.UTC)

	template := x509.Certificate{
		SerialNumber: new(big.Int).SetInt64(mr.Int63()),
		Subject: pkix.Name{
			CommonName: name,
		},
		NotBefore: notBefore,
		NotAfter:  notAfter,

		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		l.Fatalln("create cert:", err)
	}

	certOut, err := os.Create(certFile)
	if err != nil {
		l.Fatalln("save cert:", err)
	}
	err = pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: derBytes})
	if err != nil {
		l.Fatalln("save cert:", err)
	}
	err = certOut.Close()
	if err != nil {
		l.Fatalln("save cert:", err)
	}

	keyOut, err := os.OpenFile(keyFile, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		l.Fatalln("save key:", err)
	}
	err = pem.Encode(keyOut, &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(priv)})
	if err != nil {
		l.Fatalln("save key:", err)
	}
	err = keyOut.Close()
	if err != nil {
		l.Fatalln("save key:", err)
	}

	return tls.LoadX509KeyPair(certFile, keyFile)
}
