// Package smtp is the driver that transports an email through an SMTP server,
// with the standard library only (net/smtp, crypto/tls and mime/...).
//
// It is apart from package mail so only the program that sends the emails
// imports it.
package smtp
