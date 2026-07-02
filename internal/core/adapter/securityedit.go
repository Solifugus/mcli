package adapter

import (
	"fmt"
	"regexp"
	"strings"
)

// AdapterSecurityEdit is not a method interface: the DCL editing side is built as
// pure, dialect-aware string builders (GrantStatement / CreateUserStatement /
// DropUserStatement) rather than per-adapter execution, so that every generated
// statement flows back through the ONE guarded execution path (RunStatement, gated
// by the safety layer's GuardStatement) instead of a second, unguarded one. An
// adapter advertises CapSecurityEdit when its dialect is handled here. See design
// §21 (safety), §29 (Security area).

// privPattern constrains a privilege token to letters and spaces ("SELECT",
// "ALL PRIVILEGES"), so a privilege list can never smuggle syntax into the
// statement.
var privPattern = regexp.MustCompile(`^[A-Za-z][A-Za-z ]*$`)

// identPattern constrains a bare identifier (role / user / host) to a safe subset.
// DCL identifiers are admin-supplied and reviewed under the guard, but we still
// refuse anything that could break out of the statement.
var identPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_$]*$`)

// objectPattern additionally allows a schema qualifier ("schema.table") and a
// trailing ".*" (all objects in a schema, e.g. Postgres ALL TABLES form is not
// covered — this is a plain object reference).
var objectPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_$]*(\.[A-Za-z_][A-Za-z0-9_$*]*)*$`)

func validateIdent(kind, s string) error {
	if !identPattern.MatchString(s) {
		return fmt.Errorf("invalid %s %q (letters, digits, and _ only)", kind, s)
	}
	return nil
}

// GrantStatement builds a GRANT/REVOKE statement. With a non-empty object it is a
// privilege grant ("GRANT SELECT, INSERT ON schema.t TO bob"); with an empty
// object it is a role grant ("GRANT read_role TO bob") and items are role names.
// revoke swaps GRANT/TO for REVOKE/FROM. It is pure and dialect-aware (principals
// differ: MySQL uses 'user'@'host'), so both front-ends and tests share one
// builder and the result is executed through the normal guarded path.
func GrantStatement(d Dialect, items []string, object, principal string, revoke bool) (string, error) {
	if len(items) == 0 {
		return "", fmt.Errorf("no privileges or roles specified")
	}
	who, err := formatPrincipal(d, principal)
	if err != nil {
		return "", err
	}
	verb, prep := "GRANT", "TO"
	if revoke {
		verb, prep = "REVOKE", "FROM"
	}
	if object == "" {
		// Role grant: items are role identifiers.
		for _, it := range items {
			if err := validateIdent("role", it); err != nil {
				return "", err
			}
		}
		return fmt.Sprintf("%s %s %s %s", verb, strings.Join(items, ", "), prep, who), nil
	}
	// Privilege grant: items are privilege keywords.
	for _, it := range items {
		if !privPattern.MatchString(it) {
			return "", fmt.Errorf("invalid privilege %q", it)
		}
	}
	if !objectPattern.MatchString(object) {
		return "", fmt.Errorf("invalid object %q", object)
	}
	return fmt.Sprintf("%s %s ON %s %s %s", verb, strings.Join(items, ", "), object, prep, who), nil
}

// CreateUserStatement builds a dialect-correct CREATE USER/LOGIN/ROLE with a
// password. DB2 and the generic dialect have no portable form (DB2 authentication
// is external), so they return ErrUnsupported.
func CreateUserStatement(d Dialect, name, password string) (string, error) {
	if password == "" {
		return "", fmt.Errorf("a password is required")
	}
	switch d {
	case DialectPostgres:
		if err := validateIdent("user", name); err != nil {
			return "", err
		}
		return fmt.Sprintf("CREATE USER %s PASSWORD '%s'", name, escapeSQLLiteral(password)), nil
	case DialectMySQL:
		who, err := formatPrincipal(d, name)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("CREATE USER %s IDENTIFIED BY '%s'", who, escapeSQLLiteral(password)), nil
	case DialectTSQL:
		if err := validateIdent("login", name); err != nil {
			return "", err
		}
		return fmt.Sprintf("CREATE LOGIN %s WITH PASSWORD = '%s'", name, escapeSQLLiteral(password)), nil
	case DialectOracle:
		if err := validateIdent("user", name); err != nil {
			return "", err
		}
		if strings.ContainsAny(password, `"`+"\n\r") {
			return "", fmt.Errorf("invalid character in Oracle password")
		}
		return fmt.Sprintf(`CREATE USER %s IDENTIFIED BY "%s"`, name, password), nil
	default:
		return "", ErrUnsupported
	}
}

// DropUserStatement builds a dialect-correct DROP USER/LOGIN/ROLE. It is a
// destructive statement — the safety guard flags DROP as dangerous, so execution
// still requires confirmation (or is blocked on production).
func DropUserStatement(d Dialect, name string) (string, error) {
	switch d {
	case DialectPostgres:
		if err := validateIdent("role", name); err != nil {
			return "", err
		}
		return "DROP ROLE " + name, nil
	case DialectMySQL:
		who, err := formatPrincipal(d, name)
		if err != nil {
			return "", err
		}
		return "DROP USER " + who, nil
	case DialectTSQL:
		if err := validateIdent("login", name); err != nil {
			return "", err
		}
		return "DROP LOGIN " + name, nil
	case DialectOracle:
		if err := validateIdent("user", name); err != nil {
			return "", err
		}
		return "DROP USER " + name, nil
	default:
		return "", ErrUnsupported
	}
}

// formatPrincipal renders a principal for a GRANT/REVOKE/CREATE/DROP. MySQL
// principals are 'user'@'host' (a bare name defaults host to '%'); every other
// dialect uses a bare validated identifier.
func formatPrincipal(d Dialect, name string) (string, error) {
	if d == DialectMySQL {
		user, host := name, "%"
		if i := strings.LastIndexByte(name, '@'); i >= 0 {
			user, host = name[:i], name[i+1:]
		}
		if err := validateIdent("user", user); err != nil {
			return "", err
		}
		// Host allows the '%' and '.' wildcards/segments of a MySQL host spec.
		if host != "%" && !regexp.MustCompile(`^[A-Za-z0-9_.%-]+$`).MatchString(host) {
			return "", fmt.Errorf("invalid host %q", host)
		}
		return "'" + user + "'@'" + host + "'", nil
	}
	if err := validateIdent("principal", name); err != nil {
		return "", err
	}
	return name, nil
}

// escapeSQLLiteral doubles single quotes for embedding in a standard SQL string
// literal.
func escapeSQLLiteral(s string) string {
	return strings.ReplaceAll(s, "'", "''")
}
