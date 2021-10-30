#ifndef __RUTA_COMMON_H_
#define __RUTA_COMMON_H_

#include <rte_eal.h>
#include <rte_ethdev.h>
#include <rte_cycles.h>
#include <rte_lcore.h>
#include <rte_malloc.h>
#include <rte_mbuf.h>
#include <rte_hash_crc.h>
#include <rte_bus_vdev.h>
#include <rte_ether.h>
#include <rte_cryptodev.h>
#include <rte_ip.h>
#include <rte_udp.h>
#include <inttypes.h>
#include <stdbool.h>
#include <signal.h>
#include <unistd.h>

#define RX_RING_SIZE 1024
#define TX_RING_SIZE 1024
#define NUM_MBUFS ((64 * 1024) - 1)
#define MBUF_CACHE_SIZE 128
#define SCHED_RX_RING_SZ 65536
#define SCHED_TX_RING_SZ 65536
#define ETH_MAX_FRAME_SIZE 9200

#define PREFETCH_OFFSET 4

//#define NUM_RX_CORE 2
//#define  NUM_MEMIF_PER_CORE 8
//#define NUM_TX_CORE 2

#define MAX_SERVICE_CORE 32

#define ETH_PORT_ID 0
#define BURST_SIZE 32
#define BURST_SIZE_TX 64
#define BURST_TX_DRAIN_US 100

static volatile bool force_quit;
#define TICKS_PER_CYCLE_SHIFT 16
static uint64_t ticks_per_cycle_mult;

typedef uint64_t tsc_t;
static int tsc_dynfield_offset = -1;
static int hwts_dynfield_offset = -1;

static inline tsc_t *
tsc_field(struct rte_mbuf *mbuf)
{
    return RTE_MBUF_DYNFIELD(mbuf, tsc_dynfield_offset, tsc_t *);
}

static const struct rte_mbuf_dynfield tsc_dynfield_desc = {
    .name = "example_bbdev_dynfield_tsc",
    .size = sizeof(tsc_t),
    .align = __alignof__(tsc_t),
};

static inline rte_mbuf_timestamp_t *
hwts_field(struct rte_mbuf *mbuf)
{
    return RTE_MBUF_DYNFIELD(mbuf,
                             hwts_dynfield_offset, rte_mbuf_timestamp_t *);
}



static void
signal_handler(int signum)
{
    if (signum == SIGINT || signum == SIGTERM) {
        printf("\n\nSignal %d received, preparing to exit...\n",
                    signum);
        force_quit = true;
    }
}

#define NIPQUAD(addr)                \
    ((unsigned char *)&addr)[0],     \
        ((unsigned char *)&addr)[1], \
        ((unsigned char *)&addr)[2], \
        ((unsigned char *)&addr)[3]



static inline int
print_pkt(struct rte_mbuf *pkt)
{

    struct rte_ether_hdr *eth_hdr;
    struct rte_ipv4_hdr *ipv4_hdr;
    struct rte_udp_hdr *udp_hdr;
    struct rte_tcp_hdr *tcp_hdr;

    eth_hdr = rte_pktmbuf_mtod(pkt, struct rte_ether_hdr *);
    printf("%02X:%02X:%02X:%02X:%02X:%02X -> %02X:%02X:%02X:%02X:%02X:%02X \n",

           eth_hdr->s_addr.addr_bytes[0], eth_hdr->s_addr.addr_bytes[1],
           eth_hdr->s_addr.addr_bytes[2], eth_hdr->s_addr.addr_bytes[3],
           eth_hdr->s_addr.addr_bytes[4], eth_hdr->s_addr.addr_bytes[5],
           eth_hdr->d_addr.addr_bytes[0], eth_hdr->d_addr.addr_bytes[1],
           eth_hdr->d_addr.addr_bytes[2], eth_hdr->d_addr.addr_bytes[3],
           eth_hdr->d_addr.addr_bytes[4], eth_hdr->d_addr.addr_bytes[5]);

    if (pkt->packet_type & RTE_PTYPE_L3_IPV4)
    {

        uint32_t l4 = pkt->packet_type & RTE_PTYPE_L4_MASK;

        ipv4_hdr = rte_pktmbuf_mtod_offset(pkt, struct rte_ipv4_hdr *, sizeof(struct rte_ether_hdr));
        if (l4 == RTE_PTYPE_L4_ICMP)
        {
            printf("ICMP: %d.%d.%d.%d->%d.%d.%d.%d\n",
                   NIPQUAD(ipv4_hdr->src_addr),
                   NIPQUAD(ipv4_hdr->dst_addr));
        }

        if (l4  == RTE_PTYPE_L4_UDP)
        {
            udp_hdr = rte_pktmbuf_mtod_offset(pkt, struct rte_udp_hdr *, sizeof(struct rte_ether_hdr) + sizeof(struct rte_ipv4_hdr));
            printf("UDP: %d.%d.%d.%d:%d->%d.%d.%d.%d:%d\n",
                   NIPQUAD(ipv4_hdr->src_addr), rte_be_to_cpu_16(udp_hdr->src_port),
                   NIPQUAD(ipv4_hdr->dst_addr), rte_be_to_cpu_16(udp_hdr->dst_port));
        }

        if (l4 == RTE_PTYPE_L4_TCP)
        {
            tcp_hdr = rte_pktmbuf_mtod_offset(pkt, struct rte_tcp_hdr *, sizeof(struct rte_ether_hdr) + sizeof(struct rte_ipv4_hdr));
            printf("TCP: %d.%d.%d.%d:%d->%d.%d.%d.%d:%d\n",
                   NIPQUAD(ipv4_hdr->src_addr), rte_be_to_cpu_16(tcp_hdr->src_port),
                   NIPQUAD(ipv4_hdr->dst_addr), rte_be_to_cpu_16(tcp_hdr->dst_port));
        }
    }
}

static inline void
parse_eth_ptype(struct rte_mbuf *m)
{
    struct rte_ether_hdr *eth_hdr;
    uint32_t packet_type = RTE_PTYPE_UNKNOWN;
    uint16_t ether_type;
    eth_hdr = rte_pktmbuf_mtod(m, struct rte_ether_hdr *);
    ether_type = eth_hdr->ether_type;

    if (ether_type == rte_cpu_to_be_16(RTE_ETHER_TYPE_IPV4))
    {
        packet_type |= RTE_PTYPE_L3_IPV4;
    }
    else if (ether_type == rte_cpu_to_be_16(RTE_ETHER_TYPE_IPV6))
    {
        packet_type |= RTE_PTYPE_L3_IPV6;
    }
    else if (ether_type == rte_cpu_to_be_16(RTE_ETHER_TYPE_ARP))
    {
        packet_type |= RTE_PTYPE_L2_ETHER_ARP;
    }
    m->packet_type = packet_type;
}

static inline void
parse_ipv4_ptype(struct rte_mbuf *m)
{
    struct rte_ipv4_hdr *ipv4_hdr;
    uint32_t packet_type =  m->packet_type;

    ipv4_hdr = rte_pktmbuf_mtod_offset(m, struct rte_ipv4_hdr *, sizeof(struct rte_ether_hdr));
    if (ipv4_hdr->next_proto_id == IPPROTO_UDP)
    {
        packet_type |= RTE_PTYPE_L4_UDP;
    }
    else if (ipv4_hdr->next_proto_id == IPPROTO_TCP)
    {
        packet_type |= RTE_PTYPE_L4_TCP;
    }
    else if (ipv4_hdr->next_proto_id == IPPROTO_ICMP)
    {
        packet_type |= RTE_PTYPE_L4_ICMP;
    }
    m->packet_type = packet_type;
}

#endif /* __RUTA_COMMON_H_ */
