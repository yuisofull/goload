package rsa

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
)

func SerializePublicKey(publicKey *rsa.PublicKey) (pemBytes []byte, err error) {
	pubASN1, err := x509.MarshalPKIXPublicKey(publicKey)
	if err != nil {
		return nil, err
	}
	var PEMBlock = &pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: pubASN1,
	}
	return pem.EncodeToMemory(PEMBlock), nil
}

func DeserializePublicKey(pemBytes []byte) (publicKey *rsa.PublicKey, err error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil || block.Type != "PUBLIC KEY" {
		return nil, errors.New("invalid PEM block or type")
	}
	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, err
	}

	rsaPub, ok := pub.(*rsa.PublicKey)
	if !ok {
		return nil, errors.New("not RSA public key")
	}

	return rsaPub, nil
}

func SerializePrivateKey(privateKey *rsa.PrivateKey) (pemBytes []byte, err error) {
	privASN1, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		return nil, err
	}
	var PEMBlock = &pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: privASN1,
	}
	return pem.EncodeToMemory(PEMBlock), nil
}

func DeserializePrivateKey(pemBytes []byte) (privateKey *rsa.PrivateKey, err error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil || block.Type != "PRIVATE KEY" {
		return nil, errors.New("invalid PEM block or type")
	}
	priv, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}

	rsaPriv, ok := priv.(*rsa.PrivateKey)
	if !ok {
		return nil, errors.New("not RSA private key")
	}

	return rsaPriv, nil
}
