package main

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
)

// checkWritable measures whether a directory really accepts a write.
//
// It creates a file and removes it, instead of reading the permission bits:
// mode bits lie under ACL, read-only mounts, containers and Windows, where a
// 755 directory can still be unwritable. The mode-bit guess is the kind of
// assumption that makes a security control a decoration (D-70).
//
// The probe file is removed even on the error path; a check that leaves litter
// in the operator's project is its own bug.
func checkWritable(dir string) error {
	f, err := os.CreateTemp(dir, ".ancientcode-prova-*")
	if err != nil {
		return err
	}
	name := f.Name()
	_ = f.Close()
	if err := os.Remove(name); err != nil {
		// Best effort: a leftover probe is noise, not a reason to fail /cd.
		_ = err
	}
	return nil
}

// notWritableMessage explains the situation and gives the fix.
//
// It names the user on purpose. "sem permissão" sends the operator hunting;
// "o usuario ancientcode precisa de acesso a esta pasta" is the thing they
// have to act on. And the commands are literal — a fix described in prose is
// a fix the operator postpones.
func notWritableMessage(root, who string) string {
	if who == "" {
		who = "o usuario do processo"
	}
	return fmt.Sprintf(
		"aviso: %s nao consegue escrever em %s.\n"+
			"  Ler funciona; escrever nao. write_file, edit_file e o shell vao falhar com\n"+
			"  \"permission denied\" la dentro, sem relacao aparente com este comando.\n"+
			"  Para dar escrita a este usuario nessa pasta:\n"+
			"    sudo chgrp -R %[2]s %[1]s\n"+
			"    sudo chmod -R g+rwX %[1]s\n"+
			"    sudo find %[1]s -type d -exec chmod g+s {} \\;\n"+
			"  O setgid (g+s) mantem o acesso: sem ele, os arquivos novos nascem sem\n"+
			"  escrita para o grupo e o problema volta sem aviso.",
		root, who)
}

// currentUserName is the login of the running process, for the message. It
// falls back to the uid rather than failing: the message is an aid, and an aid
// that crashes /cd is worse than an aid that says less.
func currentUserName() string {
	if u, err := user.Current(); err == nil && u.Username != "" {
		return u.Username
	}
	return "uid " + strconv.Itoa(os.Getuid())
}

// writableProbe is the indirection that lets a test force "not writable".
//
// Making a directory unwritable is not portable — on Windows the process is
// usually an administrator and the bit is advisory — so the only honest way to
// test the warning is to swap the measurement. Without this, three mutations
// stayed green: the warning could stop existing entirely and the suite would
// not notice.
var writableProbe = checkWritable

// writableWarning returns the warning for a root that cannot be written, or
// "" when it can.
func writableWarning(root string) string {
	if err := writableProbe(root); err != nil {
		return notWritableMessage(root, currentUserName())
	}
	return ""
}

var _ = filepath.Base
