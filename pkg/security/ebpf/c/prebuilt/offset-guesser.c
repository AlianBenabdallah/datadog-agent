#include <linux/compiler.h>

#include "kconfig.h"
#include <linux/ptrace.h>
#include <linux/types.h>
#include <linux/version.h>
#include <uapi/linux/bpf.h>
#include "bpf_tracing.h"
#include "constants.h"
#include "offset.h"

__u32 _version SEC("version") = 0xFFFFFFFE;

char LICENSE[] SEC("license") = "GPL";
