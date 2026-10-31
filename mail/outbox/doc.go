// Package outbox sends emails later and with retries: a mail.Sender that
// saves the message in a Store (implemented by the application, usually a
// database table) and a worker that claims the pending ones, sends them and
// marks them as sent or failed, with a delay between attempts.
//
// It is apart from package mail so a service that only sends emails does not
// import it.
package outbox
