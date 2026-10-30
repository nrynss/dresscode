// Package config loads Dresscode settings from one TOML file.
//
// Budgets, the judge lease, and the runtime flags sit in the file. A key
// is a reference to an environment variable or the keyring, never a value
// in the file. A missing key fails with the variable's name and never
// with a value.
package config
