//go:generate sh -c "cd ../internal/generate && go run . -output ../generated"

// Package schema owns the independent global project catalog format.
package schema

import "strings"

const Version = "1"
const ApplicationID = "github.com/MartinAndreev/conduit/project-catalog"

// ApplicationIDs is the versioned on-disk membership, checked against the domain.
var ApplicationIDs = []string{"tui", "gui"}

var Statements = []string{
	`CREATE TABLE catalog_metadata ("key" TEXT PRIMARY KEY, value TEXT NOT NULL)`,
	`CREATE TABLE projects (
  id TEXT PRIMARY KEY, name TEXT NOT NULL CHECK(length(name) > 0),
  canonical_path TEXT NOT NULL UNIQUE CHECK(length(canonical_path) > 0)
 )`,
	`CREATE TABLE last_opened (
  application_id TEXT PRIMARY KEY CHECK(application_id IN ('` + strings.Join(ApplicationIDs, "', '") + `')),
  project_id TEXT NOT NULL REFERENCES projects(id)
 )`,
}
