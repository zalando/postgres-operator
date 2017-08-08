package users

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/zalando-incubator/postgres-operator/pkg/spec"
	"github.com/zalando-incubator/postgres-operator/pkg/util"
)

const (
	createUserSQL    = `SET LOCAL synchronous_commit = 'local'; CREATE ROLE "%s" %s %s;`
	alterUserSQL     = `ALTER ROLE "%s" %s`
	grantToUserSQL   = `GRANT %s TO "%s"`
	doBlockStmt      = `SET LOCAL synchronous_commit = 'local'; DO $$ BEGIN %s; END;$$;`
	passwordTemplate = "ENCRYPTED PASSWORD '%s'"
	inRoleTemplate   = `IN ROLE %s`
)

// DefaultUserSyncStrategy implements a user sync strategy that merges already existing database users
// with those defined in the manifest, altering existing users when necessary. It will never strips
// an existing roles of another role membership, nor it removes the already assigned flag
// (except for the NOLOGIN). TODO: process other NOflags, i.e. NOSUPERUSER correctly.
type DefaultUserSyncStrategy struct {
}

// ProduceSyncRequests figures out the types of changes that need to happen with the given users.
func (s DefaultUserSyncStrategy) ProduceSyncRequests(dbUsers spec.PgUserMap,
	newUsers spec.PgUserMap) (reqs []spec.PgSyncUserRequest) {

	// No existing roles are deleted or stripped of role memebership/flags
	for name, newUser := range newUsers {
		dbUser, exists := dbUsers[name]
		if !exists {
			reqs = append(reqs, spec.PgSyncUserRequest{Kind: spec.PGSyncUserAdd, User: newUser})
		} else {
			r := spec.PgSyncUserRequest{}
			newMD5Password := util.PGUserPassword(newUser)

			if dbUser.Password != newMD5Password {
				r.User.Password = newMD5Password
				r.Kind = spec.PGsyncUserAlter
			}
			if addNewRoles, equal := util.SubstractStringSlices(newUser.MemberOf, dbUser.MemberOf); !equal {
				r.User.MemberOf = addNewRoles
				r.Kind = spec.PGsyncUserAlter
			}
			if addNewFlags, equal := util.SubstractStringSlices(newUser.Flags, dbUser.Flags); !equal {
				r.User.Flags = addNewFlags
				r.Kind = spec.PGsyncUserAlter
			}
			if r.Kind == spec.PGsyncUserAlter {
				r.User.Name = newUser.Name
				reqs = append(reqs, r)
			}
		}
	}

	return
}

// ExecuteSyncRequests makes actual database changes from the requests passed in its arguments.
func (s DefaultUserSyncStrategy) ExecuteSyncRequests(reqs []spec.PgSyncUserRequest, db *sql.DB) error {
	for _, r := range reqs {
		switch r.Kind {
		case spec.PGSyncUserAdd:
			if err := s.createPgUser(r.User, db); err != nil {
				return fmt.Errorf("could not create user %q: %v", r.User.Name, err)
			}
		case spec.PGsyncUserAlter:
			if err := s.alterPgUser(r.User, db); err != nil {
				return fmt.Errorf("could not alter user %q: %v", r.User.Name, err)
			}
		default:
			return fmt.Errorf("unrecognized operation: %v", r.Kind)
		}

	}
	return nil
}

func (s DefaultUserSyncStrategy) createPgUser(user spec.PgUser, db *sql.DB) (err error) {
	var userFlags []string
	var userPassword string

	if len(user.Flags) > 0 {
		userFlags = append(userFlags, user.Flags...)
	}
	if len(user.MemberOf) > 0 {
		userFlags = append(userFlags, fmt.Sprintf(inRoleTemplate, quoteMemberList(user)))
	}

	if user.Password == "" {
		userPassword = "PASSWORD NULL"
	} else {
		userPassword = fmt.Sprintf(passwordTemplate, util.PGUserPassword(user))
	}
	query := fmt.Sprintf(createUserSQL, user.Name, strings.Join(userFlags, " "), userPassword)

	_, err = db.Query(query) // TODO: Try several times
	if err != nil {
		err = fmt.Errorf("dB error: %v, query: %q", err, query)
		return
	}

	return
}

func (s DefaultUserSyncStrategy) alterPgUser(user spec.PgUser, db *sql.DB) (err error) {
	var resultStmt []string

	if user.Password != "" || len(user.Flags) > 0 {
		alterStmt := produceAlterStmt(user)
		resultStmt = append(resultStmt, alterStmt)
	}
	if len(user.MemberOf) > 0 {
		grantStmt := produceGrantStmt(user)
		resultStmt = append(resultStmt, grantStmt)
	}
	if len(resultStmt) == 0 {
		return nil
	}

	query := fmt.Sprintf(doBlockStmt, strings.Join(resultStmt, ";"))

	_, err = db.Query(query) // TODO: Try several times
	if err != nil {
		err = fmt.Errorf("dB error: %v query %q", err, query)
		return
	}

	return
}

func produceAlterStmt(user spec.PgUser) string {
	// ALTER ROLE ... LOGIN ENCRYPTED PASSWORD ..
	result := make([]string, 1)
	password := user.Password
	flags := user.Flags

	if password != "" {
		result = append(result, fmt.Sprintf(passwordTemplate, util.PGUserPassword(user)))
	}
	if len(flags) != 0 {
		result = append(result, strings.Join(flags, " "))
	}
	return fmt.Sprintf(alterUserSQL, user.Name, strings.Join(result, " "))
}

func produceGrantStmt(user spec.PgUser) string {
	// GRANT ROLE "foo", "bar" TO baz
	return fmt.Sprintf(grantToUserSQL, quoteMemberList(user), user.Name)
}

func quoteMemberList(user spec.PgUser) string {
	var memberof []string
	for _, member := range user.MemberOf {
		memberof = append(memberof, fmt.Sprintf(`"%s"`, member))
	}
	return strings.Join(memberof, ",")
}
