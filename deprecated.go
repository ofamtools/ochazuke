//go:build !ochazuke_strict

package ochazuke

import "time"

// The names in this file are deprecated: each still works for one release
// and then goes. Building with the ochazuke_strict tag leaves this file
// out, so any use of them fails to compile; strictcheck.Check runs that
// build in an application's tests (see strictcheck). Every name ochazuke
// deprecates goes here, so the one tag catches them all.

// UIStatus is the error tone's message kind.
//
// Deprecated: use UIError (the same wire value, "status").
const UIStatus = UIError

// InfoSink was the optional interface for the status line's plain tone.
// Every UISink now has Info.
//
// Deprecated: Info is part of UISink.
type InfoSink interface {
	Info(text string)
}

// Status is Error: the status line in its error tone. The name read as
// "any status", so plain messages posted through it were drawn as errors.
//
// Deprecated: use Error for what failed, Info for a plain message.
func (u *UI) Status(text string) { u.Error(text) }

// Status is Error: the status line in its error tone.
//
// Deprecated: use Error for what failed, Info for a plain message.
func (c Ctx) Status(text string) { c.Runtime.UI().Error(text) }

// LastStatus is the last error status recorded.
//
// Deprecated: use LastError.
func (s *RecordingSink) LastStatus() string { return s.LastError() }

// DefaultBindingTimeout was the key-sequence timeout before the
// sequence-timeout setting, whose default (0, wait until a key comes) is
// in settings/ochazuke.json. Nothing in ochazuke reads it.
//
// Deprecated: read or set the sequence-timeout setting
// (SequenceTimeoutSetting), or set Runtime.BindingTimeout in code.
const DefaultBindingTimeout = 1000 * time.Millisecond

// TerminalLeaderTimeoutSetting is the old name of sequence-timeout, which
// now governs the terminal leader as every other pending sequence:
// settings.get/set/reset, a declaration and a settings.json key under
// this name read and write sequence-timeout, warning once.
//
// Deprecated: use SequenceTimeoutSetting.
const TerminalLeaderTimeoutSetting = "terminal-leader-timeout"
