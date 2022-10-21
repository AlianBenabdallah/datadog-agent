#ifndef __SOCK_H
#define __SOCK_H

#include "kconfig.h"
#include "net/sock.h"
#include "net/inet_sock.h"

#include "defs.h"
#include "bpf_core_read.h"

// source include/linux/socket.h
#define __AF_INET   2
#define __AF_INET6 10

static __always_inline bool dns_stats_enabled() {
    __u64 val = 0;
    LOAD_CONSTANT("dns_stats_enabled", val);
    return val == ENABLED;
}

static __always_inline bool is_ipv6_enabled() {
    __u64 val = 0;
    LOAD_CONSTANT("ipv6_enabled", val);
    return val == ENABLED;
}

static __always_inline __u32 get_netns_from_sock(struct sock* sk) {
    return BPF_CORE_READ(sk, sk_net.net, ns.inum);
}

static __always_inline __u16 read_sport(struct sock* sk) {
    // try skc_num, then inet_sport
    __u16 sport = BPF_CORE_READ(sk, sk_num);
    if (sport == 0) {
        sport = BPF_CORE_READ(inet_sk(sk), inet_sport);
        sport = bpf_ntohs(sport);
    }

    return sport;
}

static __always_inline bool check_family(struct sock* sk, u16 expected_family) {
    unsigned short family = BPF_PROBE_READ(sk, sk_family);
    return family == expected_family;
}

/**
 * Reads values into a `conn_tuple_t` from a `sock`. Any values that are already set in conn_tuple_t
 * are not overwritten. Returns 1 success, 0 otherwise.
 */
static __always_inline int read_conn_tuple_partial(conn_tuple_t * t, struct sock* skp, u64 pid_tgid, metadata_mask_t type) {
    t->pid = pid_tgid >> 32;
    t->metadata = type;

    // Retrieve network namespace id first since addresses and ports may not be available for unconnected UDP
    // sends
    t->netns = get_netns_from_sock(skp);

    // Retrieve addresses
    if (check_family(skp, __AF_INET)) {
        t->metadata |= CONN_V4;
        if (t->saddr_l == 0) {
            t->saddr_l = BPF_CORE_READ(skp, sk_rcv_saddr);
        }
        if (t->saddr_l == 0) {
            t->saddr_l = BPF_CORE_READ(inet_sk(skp), inet_saddr);
        }
        if (t->daddr_l == 0) {
            t->daddr_l = BPF_CORE_READ(skp, sk_daddr);
        }

        if (!t->saddr_l || !t->daddr_l) {
            log_debug("ERR(read_conn_tuple.v4): src or dst addr not set src=%d, dst=%d\n", t->saddr_l, t->daddr_l);
            return 0;
        }
    } else if (check_family(skp, __AF_INET6)) {
        if (!is_ipv6_enabled()) {
            return 0;
        }

        if (!(t->saddr_h || t->saddr_l)) {
            *(u32*)(&t->saddr_h) = BPF_CORE_READ(skp, sk_v6_rcv_saddr.s6_addr32[0]);
            *(((u32*)(&t->saddr_h))+1) = BPF_CORE_READ(skp, sk_v6_rcv_saddr.s6_addr32[1]);
            *(u32*)(&t->saddr_l) = BPF_CORE_READ(skp, sk_v6_rcv_saddr.s6_addr32[2]);
            *(((u32*)(&t->saddr_l))+1) = BPF_CORE_READ(skp, sk_v6_rcv_saddr.s6_addr32[3]);
        }
        if (!(t->daddr_h || t->daddr_l)) {
            *(u32*)(&t->daddr_h) = BPF_CORE_READ(skp, sk_v6_daddr.s6_addr32[0]);
            *(((u32*)(&t->daddr_h))+1) = BPF_CORE_READ(skp, sk_v6_daddr.s6_addr32[1]);
            *(u32*)(&t->daddr_l) = BPF_CORE_READ(skp, sk_v6_daddr.s6_addr32[2]);
            *(((u32*)(&t->daddr_l))+1) = BPF_CORE_READ(skp, sk_v6_daddr.s6_addr32[3]);
        }

        // We can only pass 4 args to bpf_trace_printk
        // so split those 2 statements to be able to log everything
        if (!(t->saddr_h || t->saddr_l)) {
            log_debug("ERR(read_conn_tuple.v6): src addr not set: type=%d, saddr_l=%d, saddr_h=%d\n",
                      type, t->saddr_l, t->saddr_h);
            return 0;
        }

        if (!(t->daddr_h || t->daddr_l)) {
            log_debug("ERR(read_conn_tuple.v6): dst addr not set: type=%d, daddr_l=%d, daddr_h=%d\n",
                      type, t->daddr_l, t->daddr_h);
            return 0;
        }

        // Check if we can map IPv6 to IPv4
        if (is_ipv4_mapped_ipv6(t->saddr_h, t->saddr_l, t->daddr_h, t->daddr_l)) {
            t->metadata |= CONN_V4;
            t->saddr_h = 0;
            t->daddr_h = 0;
            t->saddr_l = (__u32)(t->saddr_l >> 32);
            t->daddr_l = (__u32)(t->daddr_l >> 32);
        } else {
            t->metadata |= CONN_V6;
        }
    } else {
        return 0;
    }

    // Retrieve ports
    if (t->sport == 0) {
        t->sport = read_sport(skp);
    }
    if (t->dport == 0) {
        t->dport = bpf_ntohs(BPF_CORE_READ(skp, sk_dport));
    }

    if (t->sport == 0 || t->dport == 0) {
        log_debug("ERR(read_conn_tuple.v4): src/dst port not set: src:%d, dst:%d\n", t->sport, t->dport);
        return 0;
    }

    return 1;
}

/**
 * Reads values into a `conn_tuple_t` from a `sock`. Initializes all values in conn_tuple_t to `0`. Returns 1 success, 0 otherwise.
 */
static __always_inline int read_conn_tuple(conn_tuple_t* t, struct sock* skp, u64 pid_tgid, metadata_mask_t type) {
    __bpf_memset_builtin(t, 0, sizeof(conn_tuple_t));
    return read_conn_tuple_partial(t, skp, pid_tgid, type);
}

static __always_inline u32 get_sk_cookie(struct sock *sk) {
    return bpf_get_prandom_u32();
}

static __always_inline int read_conn_tuple_partial_from_flowi4(conn_tuple_t *t, struct flowi4 *fl4, u64 pid_tgid, metadata_mask_t type) {
    t->pid = pid_tgid >> 32;
    t->metadata = type;

    t->saddr_l = BPF_CORE_READ(fl4, saddr);
    t->daddr_l = BPF_CORE_READ(fl4, daddr);

    if (!t->saddr_l || !t->daddr_l) {
        log_debug("ERR(fl4): src/dst addr not set src:%d,dst:%d\n", t->saddr_l, t->daddr_l);
        return 0;
    }

    t->sport = BPF_CORE_READ(fl4, fl4_sport);
    t->dport = BPF_CORE_READ(fl4, fl4_dport);

    if (t->sport == 0 || t->dport == 0) {
        log_debug("ERR(fl4): src/dst port not set: src:%d, dst:%d\n", t->sport, t->dport);
        return 0;
    }

    t->sport = ntohs(t->sport);
    t->dport = ntohs(t->dport);

    return 1;
}

static __always_inline int read_conn_tuple_partial_from_flowi6(conn_tuple_t *t, struct flowi6 *fl6, u64 pid_tgid, metadata_mask_t type) {
    t->pid = pid_tgid >> 32;
    t->metadata = type;

    struct in6_addr addr = BPF_CORE_READ(fl6, saddr);
    read_in6_addr(&t->saddr_h, &t->saddr_l, &addr);
    addr = BPF_CORE_READ(fl6, daddr);
    read_in6_addr(&t->daddr_h, &t->daddr_l, &addr);

    if (!(t->saddr_h || t->saddr_l)) {
        log_debug("ERR(fl6): src addr not set src_l:%d,src_h:%d\n", t->saddr_l, t->saddr_h);
        return 0;
    }
    if (!(t->daddr_h || t->daddr_l)) {
        log_debug("ERR(fl6): dst addr not set dst_l:%d,dst_h:%d\n", t->daddr_l, t->daddr_h);
        return 0;
    }

    // Check if we can map IPv6 to IPv4
    if (is_ipv4_mapped_ipv6(t->saddr_h, t->saddr_l, t->daddr_h, t->daddr_l)) {
        t->metadata |= CONN_V4;
        t->saddr_h = 0;
        t->daddr_h = 0;
        t->saddr_l = (u32)(t->saddr_l >> 32);
        t->daddr_l = (u32)(t->daddr_l >> 32);
    } else {
        t->metadata |= CONN_V6;
    }

    t->sport = BPF_CORE_READ(fl6, fl6_sport);
    t->dport = BPF_CORE_READ(fl6, fl6_dport);

    if (t->sport == 0 || t->dport == 0) {
        log_debug("ERR(fl6): src/dst port not set: src:%d, dst:%d\n", t->sport, t->dport);
        return 0;
    }

    t->sport = ntohs(t->sport);
    t->dport = ntohs(t->dport);

    return 1;
}

#endif