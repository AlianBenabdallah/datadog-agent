/* SPDX-License-Identifier: (LGPL-2.1 OR BSD-2-Clause) */
#ifndef __BPF_TRACING_H__
#define __BPF_TRACING_H__

#include "bpf_helpers.h"

/* Scan the ARCH passed in from ARCH env variable (see Makefile) */
#if defined(__TARGET_ARCH_x86)
	#define bpf_target_x86
	#define bpf_target_defined
#elif defined(__TARGET_ARCH_arm64)
	#define bpf_target_arm64
	#define bpf_target_defined
#else

/* Fall back to what the compiler says */
#if defined(__x86_64__)
	#define bpf_target_x86
	#define bpf_target_defined
#elif defined(__aarch64__)
	#define bpf_target_arm64
	#define bpf_target_defined
#endif /* no compiler target */

#endif

#ifndef __BPF_TARGET_MISSING
#define __BPF_TARGET_MISSING "GCC error \"Must specify a BPF target arch via __TARGET_ARCH_xxx\""
#endif

#if defined(bpf_target_x86)

#if defined(__KERNEL__) || defined(__VMLINUX_H__)

#define PT_REGS_STACK_PARM(x,n)                                            \
({                                                                         \
    unsigned long p = 0;                                                   \
    bpf_probe_read_kernel(&p, sizeof(p), ((unsigned long *)x->sp) + n);    \
    p;                                                                     \
})

#define __PT_PARM1_REG di
#define __PT_PARM2_REG si
#define __PT_PARM3_REG dx
#define __PT_PARM4_REG cx
#define __PT_PARM5_REG r8
#define __PT_PARM6_REG r9
#define PT_REGS_PARM7(x) PT_REGS_STACK_PARM(x,1)
#define PT_REGS_PARM8(x) PT_REGS_STACK_PARM(x,2)
#define PT_REGS_PARM9(x) PT_REGS_STACK_PARM(x,3)
#define __PT_RET_REG sp
#define __PT_FP_REG bp
#define __PT_RC_REG ax
#define __PT_SP_REG sp
#define __PT_IP_REG ip
/* syscall uses r10 for PARM4 */
#define PT_REGS_PARM4_SYSCALL(x) ((x)->r10)

#else

#define PT_REGS_STACK_PARM(x,n)                                            \
({                                                                         \
    unsigned long p = 0;                                                   \
    bpf_probe_read_kernel(&p, sizeof(p), ((unsigned long *)x->rsp) + n);    \
    p;                                                                     \
})

#define __PT_PARM1_REG rdi
#define __PT_PARM2_REG rsi
#define __PT_PARM3_REG rdx
#define __PT_PARM4_REG rcx
#define __PT_PARM5_REG r8
#define __PT_PARM6_REG r9
#define PT_REGS_PARM7(x) PT_REGS_STACK_PARM(x,1)
#define PT_REGS_PARM8(x) PT_REGS_STACK_PARM(x,2)
#define PT_REGS_PARM9(x) PT_REGS_STACK_PARM(x,3)
#define __PT_RET_REG rsp
#define __PT_FP_REG rbp
#define __PT_RC_REG rax
#define __PT_SP_REG rsp
#define __PT_IP_REG rip
/* syscall uses r10 for PARM4 */
#define PT_REGS_PARM4_SYSCALL(x) ((x)->r10)

#endif /* __KERNEL__ || __VMLINUX_H__ */

#elif defined(bpf_target_arm64)

#define PT_REGS_STACK_PARM(x,n)                                            \
({                                                                         \
    unsigned long p = 0;                                                   \
    bpf_probe_read_kernel(&p, sizeof(p), ((unsigned long *)x->sp) + n);    \
    p;                                                                     \
})

struct pt_regs___arm64 {
	unsigned long orig_x0;
};

/* arm64 provides struct user_pt_regs instead of struct pt_regs to userspace */
#define __PT_REGS_CAST(x) ((const struct user_pt_regs *)(x))
#define __PT_PARM1_REG regs[0]
#define __PT_PARM2_REG regs[1]
#define __PT_PARM3_REG regs[2]
#define __PT_PARM4_REG regs[3]
#define __PT_PARM5_REG regs[4]
#define __PT_PARM6_REG regs[5]
#define PT_REGS_PARM7(x) (__PT_REGS_CAST(x)->regs[6])
#define PT_REGS_PARM8(x) (__PT_REGS_CAST(x)->regs[7])
#define PT_REGS_PARM9(x) PT_REGS_STACK_PARM(__PT_REGS_CAST(x),1)
#define __PT_RET_REG regs[30]
#define __PT_FP_REG regs[29]	/* Works only with CONFIG_FRAME_POINTER */
#define __PT_RC_REG regs[0]
#define __PT_SP_REG sp
#define __PT_IP_REG pc

#endif

#if defined(bpf_target_defined)

struct pt_regs;

/* allow some architecutres to override `struct pt_regs` */
#ifndef __PT_REGS_CAST
#define __PT_REGS_CAST(x) (x)
#endif

#define PT_REGS_PARM1(x) (__PT_REGS_CAST(x)->__PT_PARM1_REG)
#define PT_REGS_PARM2(x) (__PT_REGS_CAST(x)->__PT_PARM2_REG)
#define PT_REGS_PARM3(x) (__PT_REGS_CAST(x)->__PT_PARM3_REG)
#define PT_REGS_PARM4(x) (__PT_REGS_CAST(x)->__PT_PARM4_REG)
#define PT_REGS_PARM5(x) (__PT_REGS_CAST(x)->__PT_PARM5_REG)
#define PT_REGS_PARM6(x) (__PT_REGS_CAST(x)->__PT_PARM6_REG)
#define PT_REGS_RET(x) (__PT_REGS_CAST(x)->__PT_RET_REG)
#define PT_REGS_FP(x) (__PT_REGS_CAST(x)->__PT_FP_REG)
#define PT_REGS_RC(x) (__PT_REGS_CAST(x)->__PT_RC_REG)
#define PT_REGS_SP(x) (__PT_REGS_CAST(x)->__PT_SP_REG)
#define PT_REGS_IP(x) (__PT_REGS_CAST(x)->__PT_IP_REG)

#define BPF_KPROBE_READ_RET_IP(ip, ctx)					    \
	({ bpf_probe_read_kernel(&(ip), sizeof(ip), (void *)PT_REGS_RET(ctx)); })
#define BPF_KRETPROBE_READ_RET_IP(ip, ctx)				    \
	({ bpf_probe_read_kernel(&(ip), sizeof(ip), (void *)(PT_REGS_FP(ctx) + sizeof(ip))); })

#ifndef PT_REGS_PARM1_SYSCALL
#define PT_REGS_PARM1_SYSCALL(x) PT_REGS_PARM1(x)
#endif
#define PT_REGS_PARM2_SYSCALL(x) PT_REGS_PARM2(x)
#define PT_REGS_PARM3_SYSCALL(x) PT_REGS_PARM3(x)
#ifndef PT_REGS_PARM4_SYSCALL
#define PT_REGS_PARM4_SYSCALL(x) PT_REGS_PARM4(x)
#endif
#define PT_REGS_PARM5_SYSCALL(x) PT_REGS_PARM5(x)

#else /* defined(bpf_target_defined) */

#define PT_REGS_PARM1(x) ({ _Pragma(__BPF_TARGET_MISSING); 0l; })
#define PT_REGS_PARM2(x) ({ _Pragma(__BPF_TARGET_MISSING); 0l; })
#define PT_REGS_PARM3(x) ({ _Pragma(__BPF_TARGET_MISSING); 0l; })
#define PT_REGS_PARM4(x) ({ _Pragma(__BPF_TARGET_MISSING); 0l; })
#define PT_REGS_PARM5(x) ({ _Pragma(__BPF_TARGET_MISSING); 0l; })
#define PT_REGS_PARM6(x) ({ _Pragma(__BPF_TARGET_MISSING); 0l; })
#define PT_REGS_RET(x) ({ _Pragma(__BPF_TARGET_MISSING); 0l; })
#define PT_REGS_FP(x) ({ _Pragma(__BPF_TARGET_MISSING); 0l; })
#define PT_REGS_RC(x) ({ _Pragma(__BPF_TARGET_MISSING); 0l; })
#define PT_REGS_SP(x) ({ _Pragma(__BPF_TARGET_MISSING); 0l; })
#define PT_REGS_IP(x) ({ _Pragma(__BPF_TARGET_MISSING); 0l; })

#define BPF_KPROBE_READ_RET_IP(ip, ctx) ({ _Pragma(__BPF_TARGET_MISSING); 0l; })
#define BPF_KRETPROBE_READ_RET_IP(ip, ctx) ({ _Pragma(__BPF_TARGET_MISSING); 0l; })

#define PT_REGS_PARM1_SYSCALL(x) ({ _Pragma(__BPF_TARGET_MISSING); 0l; })
#define PT_REGS_PARM2_SYSCALL(x) ({ _Pragma(__BPF_TARGET_MISSING); 0l; })
#define PT_REGS_PARM3_SYSCALL(x) ({ _Pragma(__BPF_TARGET_MISSING); 0l; })
#define PT_REGS_PARM4_SYSCALL(x) ({ _Pragma(__BPF_TARGET_MISSING); 0l; })
#define PT_REGS_PARM5_SYSCALL(x) ({ _Pragma(__BPF_TARGET_MISSING); 0l; })

#endif /* defined(bpf_target_defined) */

/*
 * When invoked from a syscall handler kprobe, returns a pointer to a
 * struct pt_regs containing syscall arguments and suitable for passing to
 * PT_REGS_PARMn_SYSCALL() and PT_REGS_PARMn_CORE_SYSCALL().
 */
#ifndef PT_REGS_SYSCALL_REGS
/* By default, assume that the arch selects ARCH_HAS_SYSCALL_WRAPPER. */
#define PT_REGS_SYSCALL_REGS(ctx) ((struct pt_regs *)PT_REGS_PARM1(ctx))
#endif

#ifndef ___bpf_concat
#define ___bpf_concat(a, b) a ## b
#endif
#ifndef ___bpf_apply
#define ___bpf_apply(fn, n) ___bpf_concat(fn, n)
#endif
#ifndef ___bpf_nth
#define ___bpf_nth(_, _1, _2, _3, _4, _5, _6, _7, _8, _9, _a, _b, _c, N, ...) N
#endif
#ifndef ___bpf_narg
#define ___bpf_narg(...) ___bpf_nth(_, ##__VA_ARGS__, 12, 11, 10, 9, 8, 7, 6, 5, 4, 3, 2, 1, 0)
#endif

#define ___bpf_ctx_cast0()            ctx
#define ___bpf_ctx_cast1(x)           ___bpf_ctx_cast0(), (void *)ctx[0]
#define ___bpf_ctx_cast2(x, args...)  ___bpf_ctx_cast1(args), (void *)ctx[1]
#define ___bpf_ctx_cast3(x, args...)  ___bpf_ctx_cast2(args), (void *)ctx[2]
#define ___bpf_ctx_cast4(x, args...)  ___bpf_ctx_cast3(args), (void *)ctx[3]
#define ___bpf_ctx_cast5(x, args...)  ___bpf_ctx_cast4(args), (void *)ctx[4]
#define ___bpf_ctx_cast6(x, args...)  ___bpf_ctx_cast5(args), (void *)ctx[5]
#define ___bpf_ctx_cast7(x, args...)  ___bpf_ctx_cast6(args), (void *)ctx[6]
#define ___bpf_ctx_cast8(x, args...)  ___bpf_ctx_cast7(args), (void *)ctx[7]
#define ___bpf_ctx_cast9(x, args...)  ___bpf_ctx_cast8(args), (void *)ctx[8]
#define ___bpf_ctx_cast10(x, args...) ___bpf_ctx_cast9(args), (void *)ctx[9]
#define ___bpf_ctx_cast11(x, args...) ___bpf_ctx_cast10(args), (void *)ctx[10]
#define ___bpf_ctx_cast12(x, args...) ___bpf_ctx_cast11(args), (void *)ctx[11]
#define ___bpf_ctx_cast(args...)      ___bpf_apply(___bpf_ctx_cast, ___bpf_narg(args))(args)

/*
 * BPF_PROG is a convenience wrapper for generic tp_btf/fentry/fexit and
 * similar kinds of BPF programs, that accept input arguments as a single
 * pointer to untyped u64 array, where each u64 can actually be a typed
 * pointer or integer of different size. Instead of requring user to write
 * manual casts and work with array elements by index, BPF_PROG macro
 * allows user to declare a list of named and typed input arguments in the
 * same syntax as for normal C function. All the casting is hidden and
 * performed transparently, while user code can just assume working with
 * function arguments of specified type and name.
 *
 * Original raw context argument is preserved as well as 'ctx' argument.
 * This is useful when using BPF helpers that expect original context
 * as one of the parameters (e.g., for bpf_perf_event_output()).
 */
#define BPF_PROG(name, args...)						    \
name(unsigned long long *ctx);						    \
static __attribute__((always_inline)) typeof(name(0))			    \
____##name(unsigned long long *ctx, ##args);				    \
typeof(name(0)) name(unsigned long long *ctx)				    \
{									    \
	_Pragma("GCC diagnostic push")					    \
	_Pragma("GCC diagnostic ignored \"-Wint-conversion\"")		    \
	return ____##name(___bpf_ctx_cast(args));			    \
	_Pragma("GCC diagnostic pop")					    \
}									    \
static __attribute__((always_inline)) typeof(name(0))			    \
____##name(unsigned long long *ctx, ##args)

struct pt_regs;

#define ___bpf_kprobe_args0()           ctx
#define ___bpf_kprobe_args1(x)          ___bpf_kprobe_args0(), (void *)PT_REGS_PARM1(ctx)
#define ___bpf_kprobe_args2(x, args...) ___bpf_kprobe_args1(args), (void *)PT_REGS_PARM2(ctx)
#define ___bpf_kprobe_args3(x, args...) ___bpf_kprobe_args2(args), (void *)PT_REGS_PARM3(ctx)
#define ___bpf_kprobe_args4(x, args...) ___bpf_kprobe_args3(args), (void *)PT_REGS_PARM4(ctx)
#define ___bpf_kprobe_args5(x, args...) ___bpf_kprobe_args4(args), (void *)PT_REGS_PARM5(ctx)
#define ___bpf_kprobe_args6(x, args...) ___bpf_kprobe_args5(args), (void *)PT_REGS_PARM6(ctx)
#define ___bpf_kprobe_args7(x, args...) ___bpf_kprobe_args6(args), (void *)PT_REGS_PARM7(ctx)
#define ___bpf_kprobe_args8(x, args...) ___bpf_kprobe_args7(args), (void *)PT_REGS_PARM8(ctx)
#define ___bpf_kprobe_args9(x, args...) ___bpf_kprobe_args8(args), (void *)PT_REGS_PARM9(ctx)
#define ___bpf_kprobe_args(args...)     ___bpf_apply(___bpf_kprobe_args, ___bpf_narg(args))(args)

/*
 * BPF_KPROBE serves the same purpose for kprobes as BPF_PROG for
 * tp_btf/fentry/fexit BPF programs. It hides the underlying platform-specific
 * low-level way of getting kprobe input arguments from struct pt_regs, and
 * provides a familiar typed and named function arguments syntax and
 * semantics of accessing kprobe input paremeters.
 *
 * Original struct pt_regs* context is preserved as 'ctx' argument. This might
 * be necessary when using BPF helpers like bpf_perf_event_output().
 */
#define BPF_KPROBE(name, args...)					    \
name(struct pt_regs *ctx);						    \
static __attribute__((always_inline)) typeof(name(0))			    \
____##name(struct pt_regs *ctx, ##args);				    \
typeof(name(0)) name(struct pt_regs *ctx)				    \
{									    \
	_Pragma("GCC diagnostic push")					    \
	_Pragma("GCC diagnostic ignored \"-Wint-conversion\"")		    \
	return ____##name(___bpf_kprobe_args(args));			    \
	_Pragma("GCC diagnostic pop")					    \
}									    \
static __attribute__((always_inline)) typeof(name(0))			    \
____##name(struct pt_regs *ctx, ##args)

#define ___bpf_kretprobe_args0()       ctx
#define ___bpf_kretprobe_args1(x)      ___bpf_kretprobe_args0(), (void *)PT_REGS_RC(ctx)
#define ___bpf_kretprobe_args(args...) ___bpf_apply(___bpf_kretprobe_args, ___bpf_narg(args))(args)

/*
 * BPF_KRETPROBE is similar to BPF_KPROBE, except, it only provides optional
 * return value (in addition to `struct pt_regs *ctx`), but no input
 * arguments, because they will be clobbered by the time probed function
 * returns.
 */
#define BPF_KRETPROBE(name, args...)					    \
name(struct pt_regs *ctx);						    \
static __attribute__((always_inline)) typeof(name(0))			    \
____##name(struct pt_regs *ctx, ##args);				    \
typeof(name(0)) name(struct pt_regs *ctx)				    \
{									    \
	_Pragma("GCC diagnostic push")					    \
	_Pragma("GCC diagnostic ignored \"-Wint-conversion\"")		    \
	return ____##name(___bpf_kretprobe_args(args));			    \
	_Pragma("GCC diagnostic pop")					    \
}									    \
static __always_inline typeof(name(0)) ____##name(struct pt_regs *ctx, ##args)

#endif