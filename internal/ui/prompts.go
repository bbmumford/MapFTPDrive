package ui

import (
	"fmt"
	"time"

	"mapftpdrive/internal/security"
)

// Prompt texts shared by the dialogs. Fingerprints are SHA-256 throughout:
// OpenSSH's "SHA256:..." form for host keys, colon-separated hexadecimal
// for certificates.

const plainFTPPrompt = "FTP sends your password and every file without encryption, so anyone on the network path can read or change them.\n\nPrefer SFTP or FTPS. Map this drive with plain FTP anyway?"

func hostKeyPrompt(host string, key security.HostKey) string {
	return fmt.Sprintf(
		"First connection to %s.\n\nHost key type: %s\nSHA-256 fingerprint: %s\n\nCompare the fingerprint with one your server administrator gave you. Trust and pin this host key?",
		host, key.Algorithm, key.Fingerprint)
}

func hostKeyChangePrompt(host string, pinned, presented security.HostKey) string {
	return fmt.Sprintf(
		"WARNING: the host key of %s has changed.\n\nPinned:    %s (%s)\nPresented: %s (%s)\n\nThis happens when the server is reinstalled or its keys are replaced, but it is also what an attacker intercepting the connection would cause. Pin the new key only if you know why it changed. Replace the pinned key?",
		host, pinned.Fingerprint, pinned.Algorithm, presented.Fingerprint, presented.Algorithm)
}

func certificateDetails(certificate security.Certificate) string {
	return fmt.Sprintf("Subject: %s\nIssuer: %s\nValid: %s to %s\nSHA-256: %s",
		certificate.Subject, certificate.Issuer,
		certificate.NotBefore.Format(time.DateOnly), certificate.NotAfter.Format(time.DateOnly),
		security.FormatSHA256(certificate.SHA256))
}

func certificatePrompt(host string, certificate security.Certificate) string {
	reason := certificate.TrustError
	if reason == "" {
		reason = "it is not issued by an authority Windows trusts"
	}
	return fmt.Sprintf(
		"Windows does not trust the FTPS certificate of %s (%s).\n\n%s\n\nCompare the SHA-256 fingerprint with one your server administrator gave you. Trust and pin this exact certificate?",
		host, reason, certificateDetails(certificate))
}

func certificateChangePrompt(host, pinned string, presented security.Certificate) string {
	return fmt.Sprintf(
		"WARNING: the FTPS certificate of %s has changed.\n\nPinned SHA-256:    %s\n\nPresented certificate:\n%s\n\nThis happens when the certificate is renewed, but it is also what an attacker intercepting the connection would cause. Replace the pinned certificate?",
		host, security.FormatSHA256(pinned), certificateDetails(presented))
}
