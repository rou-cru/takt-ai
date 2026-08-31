// Copyright (C) 2025 Takt AI Contributors
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

package shared

// SensitivePathGlobs are the relative glob fragments every target adapter
// must deny read/write access to. Each adapter prefixes "**/" and wraps the
// fragment in its own rule syntax.
var SensitivePathGlobs = []string{
	".env",
	".env.*",
	".ssh/**",
	".credentials/**",
	"Library/Keychains/**",
	".aws/credentials",
	".config/gh/hosts.yml",
	"*.pem",
	"*.key",
	"secrets/**",
	"credentials.json",
}
