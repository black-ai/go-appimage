// TODO: Discuss with AppImage team whether we can switch from GPG to RSA
// and whether this would simplify things and reduce dependencies
// https://socketloop.com/tutorials/golang-saving-private-and-public-key-to-files

package helpers

import (
	"bytes"
	"crypto"
	"errors"
	"fmt"
	"strings"

	//	"io/ioutil"
	"log"
	"os"

	"golang.org/x/crypto/openpgp"
	"golang.org/x/crypto/openpgp/armor"
	"golang.org/x/crypto/openpgp/packet"
)

func CreateAndValidateKeyPair() {
	createKeyPair()

	// error reading armored key openpgp:
	// invalid data: entity without any identities
	// b, err := ioutil.ReadFile("privkey")
	// if err != nil {
	// 	fmt.Println(err)
	// 	return
	// }
	// hexstring, _ := readPGP(b)
	// if err != nil {
	// 	log.Fatal(err)
	// }
	// err = validate(hexstring)
	// if err != nil {
	// 	log.Fatal(err)
	// }
}

func createKeyPair() {
	conf := &packet.Config{
		RSABits: 4096,
	}
	entity, err := openpgp.NewEntity("Test Key", "Autogenerated GPG Key", "test@example.com", conf)
	if err != nil {
		log.Fatalf("error in entity.PrivateKey.Serialize(serializedEntity): %s", err)
	}
	// Generate private key and write it to a file
	serializedEntity := bytes.NewBuffer(nil)
	err = entity.PrivateKey.Serialize(serializedEntity)

	buf := bytes.NewBuffer(nil)
	headers := map[string]string{"Version": "GnuPG v1"}
	w, err := armor.Encode(buf, openpgp.PrivateKeyType, headers)
	if err != nil {
		log.Fatal(err)
	}
	_, err = w.Write(serializedEntity.Bytes())
	if err != nil {
		log.Fatalf("error armoring serializedEntity: %s", err)
	}
	w.Close()

	prf, err := os.Create(PrivkeyFileName)
	PrintError("ogpg", err)
	defer prf.Close()
	n2, err := prf.Write(buf.Bytes())
	PrintError("ogpg", err)

	fmt.Printf("wrote %d bytes\n", n2)

	// Generate public key and write it to a file
	serializedEntity = bytes.NewBuffer(nil)
	err = entity.PrimaryKey.Serialize(serializedEntity)
	if err != nil {
		log.Fatal(err)
	}
	buf = bytes.NewBuffer(nil)
	headers = map[string]string{"Version": "GnuPG v1"}
	w, err = armor.Encode(buf, openpgp.PublicKeyType, headers)
	if err != nil {
		log.Fatal(err)
	}
	_, err = w.Write(serializedEntity.Bytes())
	if err != nil {
		log.Fatalf("error armoring serializedEntity: %s", err)
	}
	w.Close()

	puf, err := os.Create(PubkeyFileName)
	PrintError("ogpg", err)
	defer puf.Close()
	n2, err = puf.Write(buf.Bytes())
	PrintError("ogpg", err)
	fmt.Printf("wrote %d bytes\n", n2)

}

// CheckSignature checks the signature embedded in an AppImage at path,
// returns the entity that has signed the AppImage and error
// based on https://stackoverflow.com/a/34008326
func CheckSignature(path string) (*openpgp.Entity, error) {
	var ent *openpgp.Entity
	err := errors.New("could not verify AppImage signature") // Be pessimistic by default, unless we can positively verify the signature
	pubkeybytes, err := GetSectionData(path, ".sig_key")

	keyring, err := openpgp.ReadArmoredKeyRing(bytes.NewReader(pubkeybytes))
	if err != nil {
		return ent, err
	}

	sigbytes, err := GetSectionData(path, ".sha256_sig")

	ent, err = openpgp.CheckArmoredDetachedSignature(keyring, strings.NewReader(CalculateSHA256Digest(path)), bytes.NewReader(sigbytes))
	if err != nil {
		return ent, err
	}

	return ent, nil
}

// SignAppImage signs an AppImage (wip), returns error - NOT TESTED YET
// Based on https://gist.github.com/eliquious/9e96017f47d9bd43cdf9
func SignAppImage(path string, digest string) error {

	in, err := os.Open(PubkeyFileName)
	defer in.Close()
	if err != nil {
		fmt.Println("Error opening public key:", err)
		return err
	}

	block, err := armor.Decode(in)
	if err != nil {
		fmt.Println("Error decoding OpenPGP Armor for public key:", err)
		return err
	}

	reader := packet.NewReader(block.Body)
	pkt, err := reader.Next()
	if err != nil {
		fmt.Println("Error reading private key:", err)
		return err
	}

	pubKey, ok := pkt.(*packet.PublicKey)
	if !ok {
		fmt.Println("Error parsing public key:", pubKey)
		return errors.New("Error parsing public key")
	}

	// open ascii armored private key
	in, err = os.Open(PrivkeyFileName)
	defer in.Close()
	if err != nil {
		fmt.Println("Error opening private key:", err)
		return err
	}

	block, err = armor.Decode(in)
	if err != nil {
		fmt.Println("Error decoding OpenPGP Armor for private key:", err)
		return err
	}

	if block.Type != openpgp.PrivateKeyType {
		fmt.Println("Error parsing private key:", block.Type)
		return errors.New("Error parsing private key")
	}

	reader = packet.NewReader(block.Body)
	pkt, err = reader.Next()
	if err != nil {
		fmt.Println("Error reading private key:", err)
		return err
	}

	privKey, ok := pkt.(*packet.PrivateKey)
	if !ok {
		fmt.Println("Error parsing private key:", pubKey)
		return errors.New("Error parsing private key")
	}

	signer := createEntityFromKeys(pubKey, privKey)

	buf := new(bytes.Buffer)

	// Get the digest we want to sign into an io.Reader
	// FIXME: Use the digest we have already calculated earlier on (let's not do it twice)
	whatToSignReader := strings.NewReader(digest)

	err = openpgp.ArmoredDetachSign(buf, signer, whatToSignReader, nil)
	if err != nil {
		fmt.Println("Error signing input:", err)
		return err
	}

	err = EmbedStringInSegment(path, ".sha256_sig", buf.String())
	if err != nil {
		PrintError("EmbedStringInSegment", err)
		return err
	}
	return nil
}

// Based on https://gist.github.com/eliquious/9e96017f47d9bd43cdf9
func createEntityFromKeys(pubKey *packet.PublicKey, privKey *packet.PrivateKey) *openpgp.Entity {

	config := packet.Config{
		DefaultHash:            crypto.SHA256,
		DefaultCipher:          packet.CipherAES256,
		DefaultCompressionAlgo: packet.CompressionZLIB,
		CompressionConfig: &packet.CompressionConfig{
			Level: 9,
		},
		RSABits: 4096,
	}
	currentTime := config.Now()
	uid := packet.NewUserId("", "", "") // FIXME: use some Travis CI and/or GitHub variables here for name and email?

	e := openpgp.Entity{
		PrimaryKey: pubKey,
		PrivateKey: privKey,
		Identities: make(map[string]*openpgp.Identity),
	}
	isPrimaryId := false

	e.Identities[uid.Id] = &openpgp.Identity{
		Name:   uid.Name,
		UserId: uid,
		SelfSignature: &packet.Signature{
			CreationTime: currentTime,
			SigType:      packet.SigTypePositiveCert,
			PubKeyAlgo:   packet.PubKeyAlgoRSA,
			Hash:         config.Hash(),
			IsPrimaryId:  &isPrimaryId,
			FlagsValid:   true,
			FlagSign:     true,
			FlagCertify:  true,
			IssuerKeyId:  &e.PrimaryKey.KeyId,
		},
	}

	keyLifetimeSecs := uint32(86400 * 365)

	e.Subkeys = make([]openpgp.Subkey, 1)
	e.Subkeys[0] = openpgp.Subkey{
		PublicKey:  pubKey,
		PrivateKey: privKey,
		Sig: &packet.Signature{
			CreationTime:              currentTime,
			SigType:                   packet.SigTypeSubkeyBinding,
			PubKeyAlgo:                packet.PubKeyAlgoRSA,
			Hash:                      config.Hash(),
			PreferredHash:             []uint8{8}, // SHA-256
			FlagsValid:                true,
			FlagEncryptStorage:        true,
			FlagEncryptCommunications: true,
			IssuerKeyId:               &e.PrimaryKey.KeyId,
			KeyLifetimeSecs:           &keyLifetimeSecs,
		},
	}
	return &e
}
