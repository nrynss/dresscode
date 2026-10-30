// Package privacy deletes a photo, a look, the style history, or an account.
//
// Account deletion is one Keel erase over the stored blobs, every table
// store.UserTables names, the identity rows, and the account's Lambo
// session. Repeating a delete is safe, and a restart resumes the erasure
// from the targets that have not confirmed. Photo, look, and history
// deletion are Dresscode jobs whose progress stage is erase. This package
// does not dial.
package privacy
