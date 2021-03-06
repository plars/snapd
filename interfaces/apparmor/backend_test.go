// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License version 3 as
 * published by the Free Software Foundation.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 *
 */

package apparmor_test

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/backendtest"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
)

type backendSuite struct {
	backendtest.BackendSuite

	parserCmd *testutil.MockCmd
}

var _ = Suite(&backendSuite{})

// fakeAppAprmorParser contains shell program that creates fake binary cache entries
// in accordance with what real apparmor_parser would do.
const fakeAppArmorParser = `
cache_dir=""
profile=""
write=""
while [ -n "$1" ]; do
	case "$1" in
		--cache-loc=*)
			cache_dir="$(echo "$1" | cut -d = -f 2)" || exit 1
			;;
		--write-cache)
			write=yes
			;;
		--replace|--remove)
			# Ignore
			;;
		-O)
			# Ignore, discard argument
			shift
			;;
		*)
			profile=$(basename "$1")
			;;
	esac
	shift
done
if [ "$write" = yes ]; then
	echo fake > "$cache_dir/$profile"
fi
`

func (s *backendSuite) SetUpTest(c *C) {
	s.Backend = &apparmor.Backend{}
	s.BackendSuite.SetUpTest(c)

	// Prepare a directory for apparmor profiles.
	// NOTE: Normally this is a part of the OS snap.
	err := os.MkdirAll(dirs.SnapAppArmorDir, 0700)
	c.Assert(err, IsNil)
	err = os.MkdirAll(dirs.AppArmorCacheDir, 0700)
	c.Assert(err, IsNil)
	// Mock away any real apparmor interaction
	s.parserCmd = testutil.MockCommand(c, "apparmor_parser", fakeAppArmorParser)
}

func (s *backendSuite) TearDownTest(c *C) {
	s.parserCmd.Restore()

	s.BackendSuite.TearDownTest(c)
}

// Tests for Setup() and Remove()

func (s *backendSuite) TestName(c *C) {
	c.Check(s.Backend.Name(), Equals, "apparmor")
}

func (s *backendSuite) TestInstallingSnapWritesAndLoadsProfiles(c *C) {
	devMode := false
	s.InstallSnap(c, devMode, backendtest.SambaYamlV1, 1)
	profile := filepath.Join(dirs.SnapAppArmorDir, "snap.samba.smbd")
	// file called "snap.sambda.smbd" was created
	_, err := os.Stat(profile)
	c.Check(err, IsNil)
	// apparmor_parser was used to load that file
	c.Check(s.parserCmd.Calls(), DeepEquals, [][]string{
		{"apparmor_parser", "--replace", "--write-cache", "-O", "no-expr-simplify", fmt.Sprintf("--cache-loc=%s/var/cache/apparmor", s.RootDir), profile},
	})
}

func (s *backendSuite) TestInstallingSnapWithHookWritesAndLoadsProfiles(c *C) {
	devMode := false
	s.InstallSnap(c, devMode, backendtest.HookYaml, 1)
	profile := filepath.Join(dirs.SnapAppArmorDir, "snap.foo.hook.configure")

	// Verify that profile "snap.foo.hook.configure" was created
	_, err := os.Stat(profile)
	c.Check(err, IsNil)
	// apparmor_parser was used to load that file
	c.Check(s.parserCmd.Calls(), DeepEquals, [][]string{
		{"apparmor_parser", "--replace", "--write-cache", "-O", "no-expr-simplify", fmt.Sprintf("--cache-loc=%s/var/cache/apparmor", s.RootDir), profile},
	})
}

func (s *backendSuite) TestProfilesAreAlwaysLoaded(c *C) {
	for _, devMode := range []bool{true, false} {
		snapInfo := s.InstallSnap(c, devMode, backendtest.SambaYamlV1, 1)
		s.parserCmd.ForgetCalls()
		err := s.Backend.Setup(snapInfo, devMode, s.Repo)
		c.Assert(err, IsNil)
		profile := filepath.Join(dirs.SnapAppArmorDir, "snap.samba.smbd")
		c.Check(s.parserCmd.Calls(), DeepEquals, [][]string{
			{"apparmor_parser", "--replace", "--write-cache", "-O", "no-expr-simplify", fmt.Sprintf("--cache-loc=%s/var/cache/apparmor", s.RootDir), profile},
		})
		s.RemoveSnap(c, snapInfo)
	}
}

func (s *backendSuite) TestRemovingSnapRemovesAndUnloadsProfiles(c *C) {
	for _, devMode := range []bool{true, false} {
		snapInfo := s.InstallSnap(c, devMode, backendtest.SambaYamlV1, 1)
		s.parserCmd.ForgetCalls()
		s.RemoveSnap(c, snapInfo)
		profile := filepath.Join(dirs.SnapAppArmorDir, "snap.samba.smbd")
		// file called "snap.sambda.smbd" was removed
		_, err := os.Stat(profile)
		c.Check(os.IsNotExist(err), Equals, true)
		// apparmor cache file was removed
		cache := filepath.Join(dirs.AppArmorCacheDir, "snap.samba.smbd")
		_, err = os.Stat(cache)
		c.Check(os.IsNotExist(err), Equals, true)
		// apparmor_parser was used to unload the profile
		c.Check(s.parserCmd.Calls(), DeepEquals, [][]string{
			{"apparmor_parser", "--remove", "snap.samba.smbd"},
		})
	}
}

func (s *backendSuite) TestRemovingSnapWithHookRemovesAndUnloadsProfiles(c *C) {
	for _, devMode := range []bool{true, false} {
		snapInfo := s.InstallSnap(c, devMode, backendtest.HookYaml, 1)
		s.parserCmd.ForgetCalls()
		s.RemoveSnap(c, snapInfo)
		profile := filepath.Join(dirs.SnapAppArmorDir, "snap.foo.hook.configure")
		// file called "snap.foo.hook.configure" was removed
		_, err := os.Stat(profile)
		c.Check(os.IsNotExist(err), Equals, true)
		// apparmor cache file was removed
		cache := filepath.Join(dirs.AppArmorCacheDir, "snap.foo.hook.configure")
		_, err = os.Stat(cache)
		c.Check(os.IsNotExist(err), Equals, true)
		// apparmor_parser was used to unload the profile
		c.Check(s.parserCmd.Calls(), DeepEquals, [][]string{
			{"apparmor_parser", "--remove", "snap.foo.hook.configure"},
		})
	}
}

func (s *backendSuite) TestUpdatingSnapMakesNeccesaryChanges(c *C) {
	for _, devMode := range []bool{true, false} {
		snapInfo := s.InstallSnap(c, devMode, backendtest.SambaYamlV1, 1)
		s.parserCmd.ForgetCalls()
		snapInfo = s.UpdateSnap(c, snapInfo, devMode, backendtest.SambaYamlV1, 2)
		profile := filepath.Join(dirs.SnapAppArmorDir, "snap.samba.smbd")
		// apparmor_parser was used to reload the profile because snap revision
		// is inside the generated policy.
		c.Check(s.parserCmd.Calls(), DeepEquals, [][]string{
			{"apparmor_parser", "--replace", "--write-cache", "-O", "no-expr-simplify", fmt.Sprintf("--cache-loc=%s/var/cache/apparmor", s.RootDir), profile},
		})
		s.RemoveSnap(c, snapInfo)
	}
}

func (s *backendSuite) TestUpdatingSnapToOneWithMoreApps(c *C) {
	for _, devMode := range []bool{true, false} {
		snapInfo := s.InstallSnap(c, devMode, backendtest.SambaYamlV1, 1)
		s.parserCmd.ForgetCalls()
		// NOTE: the revision is kept the same to just test on the new application being added
		snapInfo = s.UpdateSnap(c, snapInfo, devMode, backendtest.SambaYamlV1WithNmbd, 1)
		smbdProfile := filepath.Join(dirs.SnapAppArmorDir, "snap.samba.smbd")
		nmbdProfile := filepath.Join(dirs.SnapAppArmorDir, "snap.samba.nmbd")
		// file called "snap.sambda.nmbd" was created
		_, err := os.Stat(nmbdProfile)
		c.Check(err, IsNil)
		// apparmor_parser was used to load the both profiles
		c.Check(s.parserCmd.Calls(), DeepEquals, [][]string{
			{"apparmor_parser", "--replace", "--write-cache", "-O", "no-expr-simplify", fmt.Sprintf("--cache-loc=%s/var/cache/apparmor", s.RootDir), nmbdProfile},
			{"apparmor_parser", "--replace", "--write-cache", "-O", "no-expr-simplify", fmt.Sprintf("--cache-loc=%s/var/cache/apparmor", s.RootDir), smbdProfile},
		})
		s.RemoveSnap(c, snapInfo)
	}
}

func (s *backendSuite) TestUpdatingSnapToOneWithMoreHooks(c *C) {
	for _, devMode := range []bool{true, false} {
		snapInfo := s.InstallSnap(c, devMode, backendtest.SambaYamlV1WithNmbd, 1)
		s.parserCmd.ForgetCalls()
		// NOTE: the revision is kept the same to just test on the new application being added
		snapInfo = s.UpdateSnap(c, snapInfo, devMode, backendtest.SambaYamlWithHook, 1)
		smbdProfile := filepath.Join(dirs.SnapAppArmorDir, "snap.samba.smbd")
		nmbdProfile := filepath.Join(dirs.SnapAppArmorDir, "snap.samba.nmbd")
		hookProfile := filepath.Join(dirs.SnapAppArmorDir, "snap.samba.hook.configure")

		// Verify that profile "snap.samba.hook.configure" was created
		_, err := os.Stat(hookProfile)
		c.Check(err, IsNil)
		// apparmor_parser was used to load the both profiles
		c.Check(s.parserCmd.Calls(), DeepEquals, [][]string{
			{"apparmor_parser", "--replace", "--write-cache", "-O", "no-expr-simplify", fmt.Sprintf("--cache-loc=%s/var/cache/apparmor", s.RootDir), hookProfile},
			{"apparmor_parser", "--replace", "--write-cache", "-O", "no-expr-simplify", fmt.Sprintf("--cache-loc=%s/var/cache/apparmor", s.RootDir), nmbdProfile},
			{"apparmor_parser", "--replace", "--write-cache", "-O", "no-expr-simplify", fmt.Sprintf("--cache-loc=%s/var/cache/apparmor", s.RootDir), smbdProfile},
		})
		s.RemoveSnap(c, snapInfo)
	}
}

func (s *backendSuite) TestUpdatingSnapToOneWithFewerApps(c *C) {
	for _, devMode := range []bool{true, false} {
		snapInfo := s.InstallSnap(c, devMode, backendtest.SambaYamlV1WithNmbd, 1)
		s.parserCmd.ForgetCalls()
		// NOTE: the revision is kept the same to just test on the application being removed
		snapInfo = s.UpdateSnap(c, snapInfo, devMode, backendtest.SambaYamlV1, 1)
		smbdProfile := filepath.Join(dirs.SnapAppArmorDir, "snap.samba.smbd")
		nmbdProfile := filepath.Join(dirs.SnapAppArmorDir, "snap.samba.nmbd")
		// file called "snap.sambda.nmbd" was removed
		_, err := os.Stat(nmbdProfile)
		c.Check(os.IsNotExist(err), Equals, true)
		// apparmor_parser was used to remove the unused profile
		c.Check(s.parserCmd.Calls(), DeepEquals, [][]string{
			{"apparmor_parser", "--replace", "--write-cache", "-O", "no-expr-simplify", fmt.Sprintf("--cache-loc=%s/var/cache/apparmor", s.RootDir), smbdProfile},
			{"apparmor_parser", "--remove", "snap.samba.nmbd"},
		})
		s.RemoveSnap(c, snapInfo)
	}
}

func (s *backendSuite) TestUpdatingSnapToOneWithFewerHooks(c *C) {
	for _, devMode := range []bool{true, false} {
		snapInfo := s.InstallSnap(c, devMode, backendtest.SambaYamlWithHook, 1)
		s.parserCmd.ForgetCalls()
		// NOTE: the revision is kept the same to just test on the application being removed
		snapInfo = s.UpdateSnap(c, snapInfo, devMode, backendtest.SambaYamlV1WithNmbd, 1)
		smbdProfile := filepath.Join(dirs.SnapAppArmorDir, "snap.samba.smbd")
		nmbdProfile := filepath.Join(dirs.SnapAppArmorDir, "snap.samba.nmbd")
		hookProfile := filepath.Join(dirs.SnapAppArmorDir, "snap.samba.hook.configure")

		// Verify profile "snap.samba.hook.configure" was removed
		_, err := os.Stat(hookProfile)
		c.Check(os.IsNotExist(err), Equals, true)
		// apparmor_parser was used to remove the unused profile
		c.Check(s.parserCmd.Calls(), DeepEquals, [][]string{
			{"apparmor_parser", "--replace", "--write-cache", "-O", "no-expr-simplify", fmt.Sprintf("--cache-loc=%s/var/cache/apparmor", s.RootDir), nmbdProfile},
			{"apparmor_parser", "--replace", "--write-cache", "-O", "no-expr-simplify", fmt.Sprintf("--cache-loc=%s/var/cache/apparmor", s.RootDir), smbdProfile},
			{"apparmor_parser", "--remove", "snap.samba.hook.configure"},
		})
		s.RemoveSnap(c, snapInfo)
	}
}

func (s *backendSuite) TestRealDefaultTemplateIsNormallyUsed(c *C) {
	snapInfo := snaptest.MockInfo(c, backendtest.SambaYamlV1, nil)
	// NOTE: we don't call apparmor.MockTemplate()
	err := s.Backend.Setup(snapInfo, false, s.Repo)
	c.Assert(err, IsNil)
	profile := filepath.Join(dirs.SnapAppArmorDir, "snap.samba.smbd")
	data, err := ioutil.ReadFile(profile)
	c.Assert(err, IsNil)
	for _, line := range []string{
		// NOTE: a few randomly picked lines from the real profile.  Comments
		// and empty lines are avoided as those can be discarded in the future.
		"#include <tunables/global>\n",
		"/tmp/   r,\n",
		"/sys/class/ r,\n",
	} {
		c.Assert(string(data), testutil.Contains, line)
	}
}

type combineSnippetsScenario struct {
	devMode bool
	snippet string
	content string
}

const commonPrefix = `
@{SNAP_NAME}="samba"
@{SNAP_REVISION}="1"
@{INSTALL_DIR}="/snap"`

var combineSnippetsScenarios = []combineSnippetsScenario{{
	content: commonPrefix + `
profile "snap.samba.smbd" (attach_disconnected) {

}
`,
}, {
	snippet: "snippet",
	content: commonPrefix + `
profile "snap.samba.smbd" (attach_disconnected) {
snippet
}
`,
}, {
	devMode: true,
	content: commonPrefix + `
profile "snap.samba.smbd" (attach_disconnected,complain) {

}
`,
}, {
	devMode: true,
	snippet: "snippet",
	content: commonPrefix + `
profile "snap.samba.smbd" (attach_disconnected,complain) {
snippet
}
`}}

func (s *backendSuite) TestCombineSnippets(c *C) {
	// NOTE: replace the real template with a shorter variant
	restore := apparmor.MockTemplate([]byte("\n" +
		"###VAR###\n" +
		"###PROFILEATTACH### (attach_disconnected) {\n" +
		"###SNIPPETS###\n" +
		"}\n"))
	defer restore()
	for _, scenario := range combineSnippetsScenarios {
		s.Iface.PermanentSlotSnippetCallback = func(slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
			if scenario.snippet == "" {
				return nil, nil
			}
			return []byte(scenario.snippet), nil
		}
		snapInfo := s.InstallSnap(c, scenario.devMode, backendtest.SambaYamlV1, 1)
		profile := filepath.Join(dirs.SnapAppArmorDir, "snap.samba.smbd")
		data, err := ioutil.ReadFile(profile)
		c.Assert(err, IsNil)
		c.Check(string(data), Equals, scenario.content)
		stat, err := os.Stat(profile)
		c.Check(stat.Mode(), Equals, os.FileMode(0644))
		s.RemoveSnap(c, snapInfo)
	}
}
