// Package sshshare owns temporary SSH control sockets, client configuration,
// and the foreground master's lifetime. It never provisions credentials.
package sshshare

import (
	"crypto/sha256"
	_ "embed"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"

	"devbox/internal/fsutil"
)

const Mount = "/devbox/ssh"
const RelativeRoot = "runtime/ssh"

//go:embed master.sh
var Supervisor string

var destinationPattern = regexp.MustCompile(`^([a-zA-Z0-9_][a-zA-Z0-9_.-]*@)?[a-zA-Z0-9_:][a-zA-Z0-9_.:-]*$`)
var aliasPattern = regexp.MustCompile(`^[a-zA-Z0-9_][a-zA-Z0-9_.-]*$`)

func Validate(destination string) error {
	if !destinationPattern.MatchString(destination) || len(destination) > 255 {
		return fmt.Errorf("SSH destination must be a hostname, SSH alias, or user@host; configure ports and jump hosts in SSH config")
	}
	host := destination[strings.LastIndex(destination, "@")+1:]
	if strings.Contains(host, ":") && net.ParseIP(host) == nil {
		return fmt.Errorf("invalid SSH IPv6 address; configure ports in SSH config")
	}
	return nil
}

type Connection struct {
	Root        string
	Relative    string
	Destination string
	Alias       string
	owner       *os.File
	lifetime    *os.File
	directory   *os.File
}

// Prepare is called under the environment operation lock. Each invocation gets
// a unique owner-lock inode: an abandoned supervisor must never mistake a new
// connection's lock for its original controller returning.
func Prepare(root, destination string) (_ *Connection, err error) {
	if err = Validate(destination); err != nil {
		return nil, err
	}
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(destination)))[:12]
	connections, err := fsutil.Dir(root, "c", 0700)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(connections)
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), hash+"-") {
			continue
		}
		rel := filepath.Join("c", entry.Name())
		dir, pathErr := fsutil.Path(root, rel)
		if pathErr != nil {
			return nil, pathErr
		}
		owner, openErr := os.OpenFile(filepath.Join(dir, "owner.lock"), os.O_RDWR|syscall.O_NOFOLLOW, 0)
		if openErr == nil {
			lockErr := syscall.Flock(int(owner.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
			owner.Close()
			if errors.Is(lockErr, syscall.EWOULDBLOCK) {
				return nil, fmt.Errorf("SSH connection to %s is already in use", destination)
			}
			if lockErr != nil {
				return nil, lockErr
			}
		} else if !os.IsNotExist(openErr) {
			return nil, openErr
		}
		master, masterErr := os.OpenFile(filepath.Join(dir, "master.lock"), os.O_RDWR|syscall.O_NOFOLLOW, 0)
		if masterErr == nil {
			busy, lockErr := lockHeld(master)
			master.Close()
			if lockErr != nil {
				return nil, lockErr
			}
			if busy {
				return nil, fmt.Errorf("previous SSH connection to %s is still closing; retry shortly", destination)
			}
		} else if !os.IsNotExist(masterErr) {
			return nil, masterErr
		}
		old := &Connection{Root: root, Relative: rel}
		if old.Ready() {
			return nil, fmt.Errorf("previous SSH connection to %s is still closing; retry shortly", destination)
		}
		if err = os.RemoveAll(dir); err != nil {
			return nil, err
		}
	}
	id, err := fsutil.ID()
	if err != nil {
		return nil, err
	}
	c := &Connection{Root: root, Relative: filepath.Join("c", hash+"-"+id[:8]), Destination: destination, Alias: destination}
	if !aliasPattern.MatchString(c.Alias) {
		c.Alias = "ssh-" + hash
	}
	dir, err := fsutil.Dir(root, c.Relative, 0700)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			if c.owner != nil {
				c.owner.Close()
			}
			if c.lifetime != nil {
				c.lifetime.Close()
			}
			if c.directory != nil {
				c.directory.Close()
			}
			os.RemoveAll(dir)
		}
	}()
	c.directory, err = os.Open(dir)
	if err != nil {
		return nil, err
	}
	c.owner, err = os.OpenFile(filepath.Join(dir, "owner.lock"), os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = syscall.Flock(int(c.owner.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return nil, err
	}
	c.lifetime, err = os.OpenFile(filepath.Join(dir, "master.lock"), os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	// Defaults also reject unknown aliases. ControlPath alone would silently fall
	// back to a new authentication attempt when a master disappears.
	config := "# Devbox SSH connections. Authenticate using the host devbox-neo ssh command.\nInclude /devbox/ssh/c/*/config\nHost *\n  BatchMode yes\n  ControlMaster no\n  ProxyCommand /bin/false\n"
	if err = fsutil.Write(filepath.Join(root, "config"), []byte(config), 0600); err != nil {
		return nil, err
	}
	return c, nil
}

func (c *Connection) ContainerDirectory() string { return Mount + "/" + filepath.ToSlash(c.Relative) }

// Preserve the invoking working directory for relative host SSH configuration.
// This short path names our open directory, not an inherited fd: OpenSSH closes
// extra inherited descriptors before creating its control socket.
func (c *Connection) HostDirectory() string {
	return fmt.Sprintf("/proc/%d/fd/%d", os.Getpid(), c.directory.Fd())
}
func (c *Connection) Publish() error {
	config := fmt.Sprintf("# Destination: %s\nHost %s\n  ControlPath %s/socket\n", c.Destination, c.Alias, c.ContainerDirectory())
	return fsutil.Write(filepath.Join(c.Root, c.Relative, "config"), []byte(config), 0600)
}

// Linux /proc gives the probe a short pathname even when the Devbox home is
// longer than sockaddr_un allows. Host masters also use a short proc-fd path.
func (c *Connection) Ready() bool {
	dir, err := os.Open(filepath.Join(c.Root, c.Relative))
	if err != nil {
		return false
	}
	defer dir.Close()
	socket := fmt.Sprintf("/proc/self/fd/%d/socket", dir.Fd())
	conn, err := net.DialTimeout("unix", socket, 100*time.Millisecond)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

func lockHeld(file *os.File) (bool, error) {
	err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if errors.Is(err, syscall.EWOULDBLOCK) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return false, syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
}

func (c *Connection) Close() (result error) {
	if c.owner == nil && c.lifetime == nil && c.directory == nil {
		return nil
	}
	config, err := fsutil.Path(c.Root, filepath.Join(c.Relative, "config"))
	if err == nil {
		err = os.Remove(config)
		if os.IsNotExist(err) {
			err = nil
		}
	}
	result = err
	// Withdraw discovery first. Revocation must still happen if a container has
	// modified the config path. Keep the master's lifetime inode open so cleanup
	// also waits for authentication-in-progress, which has no socket yet.
	if c.owner != nil {
		result = errors.Join(result, c.owner.Close())
		c.owner = nil
	}
	defer func() {
		if c.lifetime != nil {
			result = errors.Join(result, c.lifetime.Close())
			c.lifetime = nil
		}
		if c.directory != nil {
			result = errors.Join(result, c.directory.Close())
			c.directory = nil
		}
	}()
	deadline := time.Now().Add(5 * time.Second)
	for {
		busy := false
		if c.lifetime != nil {
			busy, err = lockHeld(c.lifetime)
			if err != nil {
				return errors.Join(result, err)
			}
		}
		if !busy && !c.Ready() {
			return result
		}
		if time.Now().After(deadline) {
			return errors.Join(result, fmt.Errorf("SSH master did not close after its controller exited"))
		}
		time.Sleep(100 * time.Millisecond)
	}
}
