// Package revoke implements the one-revoke primitive from spec §3.4.
//
// When a human or AI identity is revoked we must atomically:
//
//  1. mark the identity row inactive (callers do this via scim.Store
//     or agents.Store — revoke doesn't own the row),
//  2. invalidate in-flight authenticators (sessions for users, certs
//     for agents — also the caller's responsibility),
//  3. strip every ACL tuple where the identity is the SUBJECT so the
//     next authenticated call by anyone CANNOT land back on a resource
//     the revoked identity was authorised for.
//
// This package owns step 3. Steps 1 and 2 live in the identity stores
// so that revocation works even when FGA is unreachable, but an FGA
// cleanup failure here is surfaced as an error so the admin knows the
// principal is still wired into the ACL graph.
package revoke

import (
	"context"
	"errors"
	"fmt"

	"github.com/geirtellefsen1/dominos/packages/api-gateway/internal/acl"
)

// Principal is the "<kind>:<id>" form used throughout the codebase.
type Principal string

// SubjectTuples removes every tuple where `principal` is the subject,
// across ANY relation and ANY object type. Returns (countRemoved, err).
//
// Before Sprint 2 #11 this used ListObjects per known relation
// (owner/reader/writer) against the document type only, which:
//   - silently missed any relation or object type we hadn't enumerated
//     (forgetting a new relation = revoke coverage gap),
//   - and truncated at FGA's listObjectsMaxResults (~1000) with no
//     continuation-token support.
//
// The paginated /read endpoint solves both: it returns every tuple
// matching {user: principal} across all relations/objects, and
// supports a continuation_token loop. ReadAllTuplesForUser handles
// the paging; we just batch the resulting deletes at 100 per call
// (FGA's write cap).
func SubjectTuples(ctx context.Context, fga *acl.Client, principal Principal) (int, error) {
	if fga == nil {
		return 0, errors.New("fga client not configured; cannot revoke acl tuples")
	}
	if principal == "" {
		return 0, errors.New("empty principal")
	}

	tuples, err := fga.ReadAllTuplesForUser(ctx, string(principal))
	if err != nil {
		return 0, fmt.Errorf("read tuples for %s: %w", string(principal), err)
	}
	if len(tuples) == 0 {
		return 0, nil
	}
	total := 0
	for i := 0; i < len(tuples); i += 100 {
		end := i + 100
		if end > len(tuples) {
			end = len(tuples)
		}
		if err := fga.Delete(ctx, tuples[i:end]...); err != nil {
			return total, fmt.Errorf("delete batch: %w", err)
		}
		total += end - i
	}
	return total, nil
}
