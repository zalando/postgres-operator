package users

import (
	"database/sql"
	"fmt"
	"strings"

	"reflect"

	"github.com/zalando/postgres-operator/pkg/spec"
	"github.com/zalando/postgres-operator/pkg/util"
	"github.com/zalando/postgres-operator/pkg/util/constants"
)

const (
	createUserSQL        = `SET LOCAL synchronous_commit = 'local'; CREATE ROLE "%s" %s %s;`
	alterUserSQL         = `ALTER ROLE "%s" %s`
	alterUserRenameSQL   = `ALTER ROLE "%s" RENAME TO "%s%s"`
	alterRoleResetAllSQL = `ALTER ROLE "%s" RESET ALL`
	alterRoleSetSQL      = `ALTER ROLE "%s" SET %s TO %s`
	grantToUserSQL       = `GRANT %s TO "%s"`
	doBlockStmt          = `SET LOCAL synchronous_commit = 'local'; DO $$ BEGIN %s; END;$$;`
	passwordTemplate     = "ENCRYPTED PASSWORD '%s'"
	inRoleTemplate       = `IN ROLE %s`
	adminTemplate        = `ADMIN %s`
)

// DefaultUserSyncStrategy implements a user sync strategy that merges already existing database users
// with those defined in the manifest, altering existing users when necessary. It will never strips
// an existing roles of another role membership, nor it removes the already assigned flag
// (except for the NOLOGIN). TODO: process other NOflags, i.e. NOSUPERUSER correctly.
type DefaultUserSyncStrategy struct {
	PasswordEncryption string
	RoleDeletionSuffix string
}

// ProduceSyncRequests figures out the types of changes that need to happen with the given users.
func (strategy DefaultUserSyncStrategy) ProduceSyncRequests(dbUsers spec.PgUserMap,
	newUsers spec.PgUserMap) []spec.PgSyncUserRequest {

	var reqs []spec.PgSyncUserRequest
	for name, newUser := range newUsers {
		// do not create user that exists in DB with deletion suffix
		if newUser.Deleted {
			continue
		}
		dbUser, exists := dbUsers[name]
		if !exists {
			reqs = append(reqs, spec.PgSyncUserRequest{Kind: spec.PGSyncUserAdd, User: newUser})
			if len(newUser.Parameters) > 0 {
				reqs = append(reqs, spec.PgSyncUserRequest{Kind: spec.PGSyncAlterSet, User: newUser})
			}
		} else {
			r := spec.PgSyncUserRequest{}
			newMD5Password := util.NewEncryptor(strategy.PasswordEncryption).PGUserPassword(newUser)

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
			if len(newUser.Parameters) > 0 && !reflect.DeepEqual(dbUser.Parameters, newUser.Parameters) {
				reqs = append(reqs, spec.PgSyncUserRequest{Kind: spec.PGSyncAlterSet, User: newUser})
			}
		}
	}

	// No existing roles are deleted or stripped of role membership/flags
	// but team roles will be renamed and denied from LOGIN
	for name, dbUser := range dbUsers {
		if _, exists := newUsers[name]; !exists {
			// toggle LOGIN flag based on role deletion
			userFlags := make([]string, len(dbUser.Flags))
			userFlags = append(userFlags, dbUser.Flags...)
			if dbUser.Deleted {
				dbUser.Flags = util.StringSliceReplaceElement(dbUser.Flags, constants.RoleFlagNoLogin, constants.RoleFlagLogin)
			} else {
				dbUser.Flags = util.StringSliceReplaceElement(dbUser.Flags, constants.RoleFlagLogin, constants.RoleFlagNoLogin)
			}
			if !util.IsEqualIgnoreOrder(userFlags, dbUser.Flags) {
				reqs = append(reqs, spec.PgSyncUserRequest{Kind: spec.PGsyncUserAlter, User: dbUser})
			}

			reqs = append(reqs, spec.PgSyncUserRequest{Kind: spec.PGSyncUserRename, User: dbUser})
		}
	}
	return reqs
}

// ExecuteSyncRequests makes actual database changes from the requests passed in its arguments.
func (strategy DefaultUserSyncStrategy) ExecuteSyncRequests(requests []spec.PgSyncUserRequest, db *sql.DB) error {
	var reqretries []spec.PgSyncUserRequest
	var errors []string
	for _, request := range requests {
		switch request.Kind {
		case spec.PGSyncUserAdd:
			if err := strategy.createPgUser(request.User, db); err != nil {
				reqretries = append(reqretries, request)
				errors = append(errors, fmt.Sprintf("could not create user %q: %v", request.User.Name, err))
			}
		case spec.PGsyncUserAlter:
			if err := strategy.alterPgUser(request.User, db); err != nil {
				reqretries = append(reqretries, request)
				errors = append(errors, fmt.Sprintf("could not alter user %q: %v", request.User.Name, err))
			}
		case spec.PGSyncAlterSet:
			if err := strategy.alterPgUserSet(request.User, db); err != nil {
				reqretries = append(reqretries, request)
				errors = append(errors, fmt.Sprintf("could not set custom user %q parameters: %v", request.User.Name, err))
			}
		case spec.PGSyncUserRename:
			if err := strategy.alterPgUserRename(request.User, db); err != nil {
				reqretries = append(reqretries, request)
				errors = append(errors, fmt.Sprintf("could not rename custom user %q: %v", request.User.Name, err))
			}
		default:
			return fmt.Errorf("unrecognized operation: %v", request.Kind)
		}

	}

	// creating roles might fail if group role members are created before the parent role
	// retry adding roles as long as the number of failed attempts is shrinking
	if len(reqretries) > 0 {
		if len(reqretries) < len(requests) {
			if err := strategy.ExecuteSyncRequests(reqretries, db); err != nil {
				return err
			}
		} else {
			return fmt.Errorf("could not execute sync requests for users: %v", errors)
		}
	}

	return nil
}

func (strategy DefaultUserSyncStrategy) alterPgUserSet(user spec.PgUser, db *sql.DB) error {
	queries := produceAlterRoleSetStmts(user)
	query := fmt.Sprintf(doBlockStmt, strings.Join(queries, ";"))
	if _, err := db.Exec(query); err != nil {
		return err
	}
	return nil
}

func (strategy DefaultUserSyncStrategy) alterPgUserRename(user spec.PgUser, db *sql.DB) error {
	var query string

	// append or trim deletion suffix depending if the user has the suffix or not
	if user.Deleted {
		newName := strings.TrimSuffix(user.Name, strategy.RoleDeletionSuffix)
		query = fmt.Sprintf(alterUserRenameSQL, user.Name, newName, "")
	} else {
		query = fmt.Sprintf(alterUserRenameSQL, user.Name, user.Name, strategy.RoleDeletionSuffix)
	}

	if _, err := db.Exec(query); err != nil {
		return err
	}
	return nil
}

func (strategy DefaultUserSyncStrategy) createPgUser(user spec.PgUser, db *sql.DB) error {
	var userFlags []string
	var userPassword string

	if len(user.Flags) > 0 {
		userFlags = append(userFlags, user.Flags...)
	}
	if len(user.MemberOf) > 0 {
		userFlags = append(userFlags, fmt.Sprintf(inRoleTemplate, quoteMemberList(user)))
	}
	if user.AdminRole != "" {
		userFlags = append(userFlags, fmt.Sprintf(adminTemplate, user.AdminRole))
	}

	if user.Password == "" {
		userPassword = "PASSWORD NULL"
	} else {
		userPassword = fmt.Sprintf(passwordTemplate, util.NewEncryptor(strategy.PasswordEncryption).PGUserPassword(user))
	}
	query := fmt.Sprintf(createUserSQL, user.Name, strings.Join(userFlags, " "), userPassword)

	if _, err := db.Exec(query); err != nil { // TODO: Try several times
		return err
	}

	if len(user.Parameters) > 0 {
		if err := strategy.alterPgUserSet(user, db); err != nil {
			return fmt.Errorf("incomplete setup for user %s: %v", user.Name, err)
		}
	}

	return nil
}

func (strategy DefaultUserSyncStrategy) alterPgUser(user spec.PgUser, db *sql.DB) error {
	var resultStmt []string

	if user.Password != "" || len(user.Flags) > 0 {
		alterStmt := produceAlterStmt(user, strategy.PasswordEncryption)
		resultStmt = append(resultStmt, alterStmt)
	}
	if len(user.MemberOf) > 0 {
		grantStmt := produceGrantStmt(user)
		resultStmt = append(resultStmt, grantStmt)
	}

	if len(resultStmt) > 0 {
		query := fmt.Sprintf(doBlockStmt, strings.Join(resultStmt, ";"))

		if _, err := db.Exec(query); err != nil { // TODO: Try several times
			return err
		}
	}

	return nil
}

func produceAlterStmt(user spec.PgUser, encryption string) string {
	// ALTER ROLE ... LOGIN ENCRYPTED PASSWORD ..
	result := make([]string, 0)
	password := user.Password
	flags := user.Flags

	if password != "" {
		result = append(result, fmt.Sprintf(passwordTemplate, util.NewEncryptor(encryption).PGUserPassword(user)))
	}
	if len(flags) != 0 {
		result = append(result, strings.Join(flags, " "))
	}
	return fmt.Sprintf(alterUserSQL, user.Name, strings.Join(result, " "))
}

func produceAlterRoleSetStmts(user spec.PgUser) []string {
	result := make([]string, 0)
	result = append(result, fmt.Sprintf(alterRoleResetAllSQL, user.Name))
	for name, value := range user.Parameters {
		result = append(result, fmt.Sprintf(alterRoleSetSQL, user.Name, name, quoteParameterValue(name, value)))
	}
	return result
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

// quoteVal quotes values to be used at ALTER ROLE SET param = value if necessary
func quoteParameterValue(name, val string) string {
	start := val[0]
	end := val[len(val)-1]
	if name == "search_path" {
		// strip single quotes from the search_path. Those are required in the YAML configuration
		// to quote values containing commas, as otherwise NewFromMap would treat each comma-separated
		// part of such string as a separate map entry. However, a search_path is interpreted as a list
		// only if it is not quoted, otherwise it is treated as a single value. Therefore, we strip
		// single quotes here. Note that you can still use double quotes in order to escape schemas
		// containing spaces (but something more complex, like double quotes inside double quotes or spaces
		// in the schema name would break the parsing code in the operator.)
		if start == '\'' && end == '\'' {
			return val[1 : len(val)-1]
		}

		return val
	}
	if (start == '"' && end == '"') || (start == '\'' && end == '\'') {
		return val
	}
	return fmt.Sprintf(`'%s'`, strings.Trim(val, " "))
}
