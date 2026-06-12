package cto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"

	"cto/src/internal/db"
	"cto/src/internal/utils"

	"github.com/go-chi/chi/v5"
)

func encryptionKey() []byte {
	k := os.Getenv("CTO_ENCRYPTION_KEY")
	if len(k) >= 32 {
		return []byte(k[:32])
	}
	return []byte("zef-cto-dev-key-change-in-prod!!")
}

func encrypt(plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil
	}
	block, err := aes.NewCipher(encryptionKey())
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err = io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	sealed := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(sealed), nil
}

func decrypt(ciphertext string) (string, error) {
	if ciphertext == "" {
		return "", nil
	}
	data, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", fmt.Errorf("base64 decode: %w", err)
	}
	block, err := aes.NewCipher(encryptionKey())
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	ns := gcm.NonceSize()
	if len(data) < ns {
		return "", fmt.Errorf("ciphertext too short")
	}
	plain, err := gcm.Open(nil, data[:ns], data[ns:], nil)
	if err != nil {
		return "", fmt.Errorf("decrypt: %w", err)
	}
	return string(plain), nil
}

func UpsertCredentialsHandler(w http.ResponseWriter, r *http.Request) {
	if !ctoDBRequired(w) {
		return
	}
	id := chi.URLParam(r, "id")

	var input struct {
		Password string `json:"password"`
		SSLCert  string `json:"ssl_cert"`
		SSLKey   string `json:"ssl_key"`
		SSLCA    string `json:"ssl_ca"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		utils.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	passEnc, err := encrypt(input.Password)
	if err != nil {
		utils.WriteError(w, http.StatusInternalServerError, "encryption failed")
		return
	}
	certEnc, _ := encrypt(input.SSLCert)
	keyEnc, _ := encrypt(input.SSLKey)

	_, err = db.GetCTOPoolOrNil().Exec(r.Context(), `
		INSERT INTO public.cto_database_credentials (database_id, password_enc, ssl_cert, ssl_key, ssl_ca, updated_at)
		VALUES ($1, $2, $3, $4, $5, NOW())
		ON CONFLICT (database_id) DO UPDATE
		  SET password_enc = EXCLUDED.password_enc,
		      ssl_cert      = EXCLUDED.ssl_cert,
		      ssl_key       = EXCLUDED.ssl_key,
		      ssl_ca        = EXCLUDED.ssl_ca,
		      updated_at    = NOW()
	`, id, passEnc, certEnc, keyEnc, input.SSLCA)
	if err != nil {
		utils.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	utils.WriteJSON(w, http.StatusOK, map[string]string{"message": "credentials saved"})
}

func GetCredentialsMaskHandler(w http.ResponseWriter, r *http.Request) {
	if !ctoDBRequired(w) {
		return
	}
	id := chi.URLParam(r, "id")

	var passEnc, certEnc, keyEnc, ca string
	err := db.GetCTOPoolOrNil().QueryRow(r.Context(), `
		SELECT password_enc, ssl_cert, ssl_key, ssl_ca
		FROM public.cto_database_credentials
		WHERE database_id = $1
	`, id).Scan(&passEnc, &certEnc, &keyEnc, &ca)
	if err != nil {
		utils.WriteJSON(w, http.StatusOK, map[string]bool{
			"has_password": false,
			"has_ssl_cert": false,
			"has_ssl_key":  false,
			"has_ssl_ca":   false,
		})
		return
	}
	utils.WriteJSON(w, http.StatusOK, map[string]bool{
		"has_password": passEnc != "",
		"has_ssl_cert": certEnc != "",
		"has_ssl_key":  keyEnc != "",
		"has_ssl_ca":   ca != "",
	})
}

func DeleteCredentialsHandler(w http.ResponseWriter, r *http.Request) {
	if !ctoDBRequired(w) {
		return
	}
	id := chi.URLParam(r, "id")
	db.GetCTOPoolOrNil().Exec(r.Context(), `DELETE FROM public.cto_database_credentials WHERE database_id = $1`, id)
	utils.WriteJSON(w, http.StatusOK, map[string]string{"message": "credentials deleted"})
}
