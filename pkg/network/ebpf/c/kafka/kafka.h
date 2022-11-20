#ifndef __KAFKA_H
#define __KAFKA_H

#include "tracer.h"
//#include "http-types.h"
#include "kafka-types.h"
#include "kafka-helpers.h"
//#include "http-maps.h"
//#include "https.h"

#include <uapi/linux/ptrace.h>

static __always_inline kafka_batch_key_t kafka_get_batch_key(u64 batch_idx) {
    kafka_batch_key_t key = { 0 };
    key.cpu = bpf_get_smp_processor_id();
    key.page_num = batch_idx % KAFKA_BATCH_PAGES;
    return key;
}
//
static __always_inline void kafka_flush_batch(struct pt_regs *ctx) {
    u32 zero = 0;
    kafka_batch_state_t *batch_state = bpf_map_lookup_elem(&kafka_batch_state, &zero);
    if (batch_state == NULL) {
        log_debug("batch state is NULL");
        return;
    }
    if (batch_state->idx_to_flush == batch_state->idx) {
//        log_debug("kafka_flush_batch: batch is not ready to be flushed: idx = %d, idx_to_flush = %d", batch_state->idx, batch_state->idx_to_flush);
        // batch is not ready to be flushed
        return;
    }
//    log_debug("kafka_flush_batch: batch is ready to be flushed");

    kafka_batch_key_t key = kafka_get_batch_key(batch_state->idx_to_flush);
    kafka_batch_t *batch = bpf_map_lookup_elem(&kafka_batches, &key);
    if (batch == NULL) {
        return;
    }

    const long status = bpf_perf_event_output(ctx, &kafka_batch_events, key.cpu, batch, sizeof(kafka_batch_t));
//    log_debug("bpf_perf_event_output status: %d\n", status);
    (void)status;
    log_debug("kafka batch flushed: cpu: %d idx: %d\n", key.cpu, batch->idx);
    batch->pos = 0;
    batch_state->idx_to_flush++;
}
//
//static __always_inline int http_responding(http_transaction_t *http) {
//    return (http != NULL && http->response_status_code != 0);
//}
//
//
static __always_inline bool kafka_batch_full(kafka_batch_t *batch) {
    return batch && batch->pos == KAFKA_BATCH_SIZE;
}
//
static __always_inline void kafka_enqueue(kafka_transaction_t *kafka_transaction) {
    // Retrieve the active batch number for this CPU
    u32 zero = 0;
    kafka_batch_state_t *batch_state = bpf_map_lookup_elem(&kafka_batch_state, &zero);
    if (batch_state == NULL) {
        log_debug("batch_state is NULL");
        return;
    }
    log_debug("Found a batch_state!");

    // Retrieve the batch object
    kafka_batch_key_t key = kafka_get_batch_key(batch_state->idx);
    kafka_batch_t *batch = bpf_map_lookup_elem(&kafka_batches, &key);
    if (batch == NULL) {
        return;
    }

    if (kafka_batch_full(batch)) {
        // this scenario should never happen and indicates a bug
        // TODO: turn this into telemetry for release 7.41
        log_debug("kafka_enqueue error: dropping request because batch is full. cpu=%d batch_idx=%d\n", bpf_get_smp_processor_id(), batch->idx);
        return;
    }

    // Bounds check to make verifier happy
    if (batch->pos < 0 || batch->pos >= KAFKA_BATCH_SIZE) {
        return;
    }

    bpf_memcpy(&batch->txs[batch->pos], kafka_transaction, sizeof(kafka_transaction_t));
    log_debug("kafka_enqueue: ktx=%llx path=%s\n", kafka_transaction, kafka_transaction->request_fragment);
    log_debug("kafka transaction enqueued: cpu: %d batch_idx: %d pos: %d\n", key.cpu, batch_state->idx, batch->pos);
    batch->pos++;
    batch->idx = batch_state->idx;

    // If we have filled the batch we move to the next one
    // Notice that we don't flush it directly because we can't do so from socket filter programs.
    if (kafka_batch_full(batch)) {
        batch_state->idx++;
    }
}
//
//static __always_inline void http_begin_request(http_transaction_t *http, http_method_t method, char *buffer) {
//    http->request_method = method;
//    http->request_started = bpf_ktime_get_ns();
//    http->response_last_seen = 0;
//    http->response_status_code = 0;
//    __builtin_memcpy(&http->request_fragment, buffer, HTTP_BUFFER_SIZE);
//    log_debug("http_begin_request: htx=%llx method=%d start=%llx\n", http, http->request_method, http->request_started);
//}
//
//static __always_inline void http_begin_response(http_transaction_t *http, const char *buffer) {
//    u16 status_code = 0;
//    status_code += (buffer[HTTP_STATUS_OFFSET+0]-'0') * 100;
//    status_code += (buffer[HTTP_STATUS_OFFSET+1]-'0') * 10;
//    status_code += (buffer[HTTP_STATUS_OFFSET+2]-'0') * 1;
//    http->response_status_code = status_code;
//    log_debug("http_begin_response: htx=%llx status=%d\n", http, status_code);
//}
//
//static __always_inline void http_parse_data(char const *p, http_packet_t *packet_type, http_method_t *method) {
//    if ((p[0] == 'H') && (p[1] == 'T') && (p[2] == 'T') && (p[3] == 'P')) {
//        *packet_type = HTTP_RESPONSE;
//    } else if ((p[0] == 'G') && (p[1] == 'E') && (p[2] == 'T') && (p[3]  == ' ') && (p[4] == '/')) {
//        *packet_type = HTTP_REQUEST;
//        *method = HTTP_GET;
//    } else if ((p[0] == 'P') && (p[1] == 'O') && (p[2] == 'S') && (p[3] == 'T') && (p[4]  == ' ') && (p[5] == '/')) {
//        *packet_type = HTTP_REQUEST;
//        *method = HTTP_POST;
//    } else if ((p[0] == 'P') && (p[1] == 'U') && (p[2] == 'T') && (p[3]  == ' ') && (p[4] == '/')) {
//        *packet_type = HTTP_REQUEST;
//        *method = HTTP_PUT;
//    } else if ((p[0] == 'D') && (p[1] == 'E') && (p[2] == 'L') && (p[3] == 'E') && (p[4] == 'T') && (p[5] == 'E') && (p[6]  == ' ') && (p[7] == '/')) {
//        *packet_type = HTTP_REQUEST;
//        *method = HTTP_DELETE;
//    } else if ((p[0] == 'H') && (p[1] == 'E') && (p[2] == 'A') && (p[3] == 'D') && (p[4]  == ' ') && (p[5] == '/')) {
//        *packet_type = HTTP_REQUEST;
//        *method = HTTP_HEAD;
//    } else if ((p[0] == 'O') && (p[1] == 'P') && (p[2] == 'T') && (p[3] == 'I') && (p[4] == 'O') && (p[5] == 'N') && (p[6] == 'S') && (p[7]  == ' ') && ((p[8] == '/') || (p[8] == '*'))) {
//        *packet_type = HTTP_REQUEST;
//        *method = HTTP_OPTIONS;
//    } else if ((p[0] == 'P') && (p[1] == 'A') && (p[2] == 'T') && (p[3] == 'C') && (p[4] == 'H') && (p[5]  == ' ') && (p[6] == '/')) {
//        *packet_type = HTTP_REQUEST;
//        *method = HTTP_PATCH;
//    }
//}
//
static __always_inline bool kafka_seen_before(kafka_transaction_t *kafka, skb_info_t *skb_info) {
    if (!skb_info || !skb_info->tcp_seq) {
        return false;
    }

    // check if we've seen this TCP segment before. this can happen in the
    // context of localhost traffic where the same TCP segment can be seen
    // multiple times coming in and out from different interfaces
    return kafka->tcp_seq == skb_info->tcp_seq;
}
//
static __always_inline void kafka_update_seen_before(kafka_transaction_t *kafka_transaction, skb_info_t *skb_info) {
    if (!skb_info || !skb_info->tcp_seq) {
        return;
    }

    log_debug("kafka_update_seen_before: ktx=%llx old_seq=%llu seq=%llu\n", kafka_transaction, kafka_transaction->tcp_seq, skb_info->tcp_seq);
    kafka_transaction->tcp_seq = skb_info->tcp_seq;
}

static __always_inline kafka_transaction_t *kafka_fetch_state(kafka_transaction_t *kafka_transaction) {
//    if (packet_type == HTTP_PACKET_UNKNOWN) {
//        return bpf_map_lookup_elem(&http_in_flight, &http->tup);
//    }

    bpf_map_update_with_telemetry(kafka_in_flight, &kafka_transaction->tup, kafka_transaction, BPF_NOEXIST);
    return bpf_map_lookup_elem(&kafka_in_flight, &kafka_transaction->tup);
}

//static __always_inline bool http_should_flush_previous_state(http_transaction_t *http, http_packet_t packet_type) {
//    return (packet_type == HTTP_REQUEST && http->request_started) ||
//        (packet_type == HTTP_RESPONSE && http->response_status_code);
//}
//
//static __always_inline bool http_closed(http_transaction_t *http, skb_info_t *skb_info, u16 pre_norm_src_port) {
//    return (skb_info && skb_info->tcp_flags&(TCPHDR_FIN|TCPHDR_RST) &&
//            // This is done to avoid double flushing the same
//            // `http_transaction_t` to userspace.  In the context of a regular
//            // TCP teardown, the FIN flag will be seen in "both ways", like:
//            //
//            // server -> FIN -> client
//            // server <- FIN <- client
//            //
//            // Since we can't make any assumptions about the ordering of these
//            // events and there are no synchronization primitives available to
//            // us, the way we solve it is by storing the non-normalized src port
//            // when we start tracking a HTTP transaction and ensuring that only the
//            // FIN flag seen in the same direction will trigger the flushing event.
//            http->owned_by_src_port == pre_norm_src_port);
//}

static __always_inline int kafka_process(kafka_transaction_t *kafka_transaction, skb_info_t *skb_info, __u64 tags) {
    if (!try_parse_request_header(kafka_transaction)) {
        return 0;
    }
    if (!try_parse_request(kafka_transaction)) {
        return 0;
    }
    log_debug("kafka_transaction->topic_name: %s", kafka_transaction->topic_name);

    kafka_enqueue(kafka_transaction);
    return 0;
}

//
//// this function is called by the socket-filter program to decide whether or not we should inspect
//// the contents of a certain packet, in order to avoid the cost of processing packets that are not
//// of interest such as empty ACKs, UDP data or encrypted traffic.
static __always_inline bool kafka_allow_packet(kafka_transaction_t *kafka, struct __sk_buff* skb, skb_info_t *skb_info) {
    // we're only interested in TCP traffic
    if (!(kafka->tup.metadata&CONN_TYPE_TCP)) {
        return false;
    }

    // if payload data is empty or if this is an encrypted packet, we only
    // process it if the packet represents a TCP termination
    bool empty_payload = skb_info->data_off == skb->len;
    if (empty_payload) {
        return skb_info->tcp_flags&(TCPHDR_FIN|TCPHDR_RST);
    }

    // Check that we didn't see this tcp segment before so we won't process
    // the same traffic twice
    log_debug("Current tcp sequence: %lu", skb_info->tcp_seq);
    __u32 *last_tcp_seq = bpf_map_lookup_elem(&kafka_last_tcp_seq_per_connection, &kafka->tup);
    if (last_tcp_seq != NULL && *last_tcp_seq == skb_info->tcp_seq) {
        log_debug("Already seen this tcp sequence: %lu", *last_tcp_seq);
        return false;
    }
    bpf_map_update_with_telemetry(kafka_last_tcp_seq_per_connection, &kafka->tup, &skb_info->tcp_seq, BPF_ANY);
    return true;
}

#endif
