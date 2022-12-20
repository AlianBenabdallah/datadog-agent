#ifndef __PROTOCOL_CLASSIFICATION_SQL_DEFS_H
#define __PROTOCOL_CLASSIFICATION_SQL_DEFS_H

#include "bpf_builtins.h"

#define SQL_ALTER "ALTER"
#define SQL_CREATE "CREATE"
#define SQL_DELETE "DELETE"
#define SQL_DROP "DROP"
#define SQL_INSERT "INSERT"
#define SQL_SELECT "SELECT"
#define SQL_UPDATE "UPDATE"

// Check that we can read the amount of memory we want, then to the comparison.
// Note: we use `sizeof(command) - 1` to *not* compare with the null-terminator of
// the strings.
#define check_command(buf, command, buf_size) ( \
    ((sizeof(command) - 1) <= buf_size)         \
    && !bpf_memcmp((buf), &(command), sizeof(command) - 1))

static __always_inline bool is_sql_command(const char *buf, __u32 buf_size) {
    return check_command(buf, SQL_ALTER, buf_size)
        || check_command(buf, SQL_CREATE, buf_size)
        || check_command(buf, SQL_DELETE, buf_size)
        || check_command(buf, SQL_DROP, buf_size)
        || check_command(buf, SQL_INSERT, buf_size)
        || check_command(buf, SQL_SELECT, buf_size)
        || check_command(buf, SQL_UPDATE, buf_size);
}

#endif /*__PROTOCOL_CLASSIFICATION_SQL_DEFS_H*/
