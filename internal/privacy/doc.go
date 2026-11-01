// Package privacy deletes a photo, a look, the style history, or an account.
//
// Account deletion is one Keel erase over the stored blobs, every table
// store.UserTables names, the account's rows in Keel's job store, the
// identity rows, and the account's Lambo session. Repeating a delete is safe,
// and a restart resumes the erasure from the targets that have not confirmed.
// Photo, look, and history deletion are Dresscode jobs whose progress stage is
// erase.
//
// Two store rules hold the erasure together. A scoped deletion erases a stored
// picture only when no row it leaves behind still names it (references.go), so
// one look's deletion cannot erase the picture another live look shows. And an
// account's erase reaches Keel's jobs table, which carries no owner column and
// holds a look's outcome and a deletion's ledger (jobrows.go). This package
// does not dial.
package privacy
