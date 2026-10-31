// Package hxmail renders the emails: it registers, for each kind of Payload,
// the view (written with Hix and rendered with hx/ssr) and the subject (a
// localizer.Dict), and gives the result to a driver. It is a mail.Sender.
//
// It is apart from package mail so a service that only sends emails does not
// import hx/ssr.
package hxmail
