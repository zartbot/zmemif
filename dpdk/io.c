#include "io.h"

static inline void
memif_pre_classify_pkts(struct rte_mbuf *m, struct rte_ring *ctrl_ring, struct rte_ring *punt_ring, uint16_t memif_start_idx, uint16_t num_memif_per_core, struct rte_eth_dev_tx_buffer **tx_buffer)
{

    struct rte_ether_hdr *eth_hdr;
    eth_hdr = rte_pktmbuf_mtod(m, struct rte_ether_hdr *);
    if (eth_hdr->ether_type == rte_cpu_to_be_16(RTE_ETHER_TYPE_IPV4))
    {
        struct rte_ipv4_hdr *ipv4_hdr;
        ipv4_hdr = rte_pktmbuf_mtod_offset(m, struct rte_ipv4_hdr *, sizeof(struct rte_ether_hdr));
        if (likely(ipv4_hdr->next_proto_id == IPPROTO_UDP))
        {
            // TODO : add dst udp service port fitlering function here.
            int id = rte_hash_crc_4byte(ipv4_hdr->src_addr, 0) & ( num_memif_per_core - 1);
            rte_eth_tx_buffer(memif_start_idx+id, 0, tx_buffer[id], m);
            return;
        }
    }

    /*
    printf("------------------------------------------------------\n");
    print_pkt(m);
    printf("packet type: %d\n", m->packet_type);
    rte_hexdump(stdout, "msg dump:", m, m->data_len);
    */

    rte_ring_enqueue(ctrl_ring, (void *)m);
}

int lcore_io_rx_memif(struct io_lcore_params *p)
{
    struct rte_mbuf *pkts[BURST_SIZE];
    int ret;
    printf("Core %u doing packet RX.\n", rte_lcore_id());
    struct rte_eth_dev_tx_buffer *tx_buffer[MAX_SERVICE_CORE];

    uint64_t prev_tsc, diff_tsc, cur_tsc, timer_tsc;
    uint16_t port_id;
    const uint64_t drain_tsc = (rte_get_tsc_hz() + US_PER_S - 1) / US_PER_S *
                               BURST_TX_DRAIN_US;
    prev_tsc = 0;
    timer_tsc = 0;

    for (int i = 0; i <  p->num_memif_per_core ; i++)
    {
        /* initialize tx buffer */
        tx_buffer[i] = rte_zmalloc_socket("rx_buffer",
                                          RTE_ETH_TX_BUFFER_SIZE(BURST_SIZE * 2),
                                          0, rte_eth_dev_socket_id(p->memif_start_idx+i));
        if (tx_buffer[i] == NULL)
            rte_exit(EXIT_FAILURE, "Cannot allocate buffer for memif tx on queue %u-%u\n",
                     p->core_id, i);

        int retval = rte_eth_tx_buffer_init(tx_buffer[i], BURST_SIZE * 2);
        if (retval < 0)
            rte_exit(EXIT_FAILURE,
                     "Cannot set error callback for tx buffer on queue %u-%u\n",
                     p->core_id, i);
    }

    while (!force_quit)
    {
        cur_tsc = rte_rdtsc();
        diff_tsc = cur_tsc - prev_tsc;
        if (unlikely(diff_tsc > drain_tsc))
        {
            for (int i = 0; i < p->num_memif_per_core; i++)
            {
                rte_eth_tx_buffer_flush(p->memif_start_idx + i, 0, tx_buffer[i]);
                prev_tsc = cur_tsc;
            }
        }

        const uint16_t nb_rx = rte_eth_rx_burst(ETH_PORT_ID, p->core_id, pkts,
                                                BURST_SIZE);
        if (unlikely(nb_rx == 0))
        {
            continue;
        }

        // printf("Core %d recieve %d packes\n",rte_lcore_id(),nb_rx);
        int i;
        /* Prefetch first packets */
        for (i = 0; i < PREFETCH_OFFSET && i < nb_rx; i++)
        {
            rte_prefetch0(rte_pktmbuf_mtod(
                pkts[i], void *));
        }
        for (i = 0; i < (nb_rx - PREFETCH_OFFSET); i++)
        {
            rte_prefetch0(rte_pktmbuf_mtod(pkts[i + PREFETCH_OFFSET], void *));
            memif_pre_classify_pkts(pkts[i], p->ctrl_ring_rx, p->punt_ring, p->memif_start_idx, p->num_memif_per_core, tx_buffer);
        }

        /* Process left packets */
        for (; i < nb_rx; i++)
        {
            memif_pre_classify_pkts(pkts[i], p->ctrl_ring_rx, p->punt_ring, p->memif_start_idx, p->num_memif_per_core, tx_buffer);
        }
    }
    return 0;
}

int lcore_io_tx(struct io_lcore_params *p)
{
    printf("Core %u doing packet TX(txq: %d).\n", rte_lcore_id(), p->core_id);

    struct rte_eth_dev_tx_buffer *tx_buffer;

    uint64_t prev_tsc, diff_tsc, cur_tsc, timer_tsc;
    uint16_t port_id;
    const uint64_t drain_tsc = (rte_get_tsc_hz() + US_PER_S - 1) / US_PER_S *
                               BURST_TX_DRAIN_US;
    prev_tsc = 0;
    timer_tsc = 0;

    struct rte_mbuf *pkts[BURST_SIZE_TX];
    struct rte_mbuf *ctrl_pkts[BURST_SIZE_TX];

    // Initialize TX Buffer
    tx_buffer = rte_zmalloc_socket("tx_buffer",
                                   RTE_ETH_TX_BUFFER_SIZE(BURST_SIZE * 2 * p->num_memif_per_core), 0,
                                   rte_eth_dev_socket_id(ETH_PORT_ID));
    if (tx_buffer == NULL)
        rte_exit(EXIT_FAILURE, "Cannot allocate buffer for tx on port %u\n",
                 ETH_PORT_ID);

    int retval = rte_eth_tx_buffer_init(tx_buffer, BURST_SIZE * 2 * p->num_memif_per_core);
    if (retval < 0)
        rte_exit(EXIT_FAILURE,
                 "Cannot set error callback for tx buffer on port %u\n",
                 ETH_PORT_ID);

    while (!force_quit)
    {
        cur_tsc = rte_rdtsc();
        diff_tsc = cur_tsc - prev_tsc;
        if (unlikely(diff_tsc > drain_tsc))
        {
            rte_eth_tx_buffer_flush(ETH_PORT_ID, p->core_id, tx_buffer);
            prev_tsc = cur_tsc;
        }

        for (uint16_t idx = 0; idx <  p->num_memif_per_core; idx++)
        {
            const uint16_t nb_rx = rte_eth_rx_burst(p->memif_start_idx + idx, 0, pkts,BURST_SIZE);
            if (unlikely(nb_rx == 0))
            {
                continue;
            }
            int i;
            /* Prefetch first packets */
            for (i = 0; i < PREFETCH_OFFSET && i < nb_rx; i++)
            {
                rte_prefetch0(rte_pktmbuf_mtod(pkts[i], void *));
            }
            for (i = 0; i < (nb_rx - PREFETCH_OFFSET); i++)
            {
                rte_prefetch0(rte_pktmbuf_mtod(pkts[i + PREFETCH_OFFSET], void *));
                //You may add some additional packet filtering or segment routing function here.
                rte_eth_tx_buffer(ETH_PORT_ID, p->core_id, tx_buffer, pkts[i]);
            }

            /* Process left packets */
            for (; i < nb_rx; i++)
            {
                rte_eth_tx_buffer(ETH_PORT_ID, p->core_id, tx_buffer, pkts[i]);
            }
        }

        // control port
        if (p->core_id == 0)
        {
            const uint16_t nb_ctrl_rx = rte_ring_dequeue_burst(p->ctrl_ring_rx,
                                                               (void *)ctrl_pkts, BURST_SIZE, NULL);
            unsigned int nb_ctrl_tx = rte_eth_tx_burst(p->ctrl_port_id, 0, ctrl_pkts, nb_ctrl_rx);
            if (unlikely(nb_ctrl_tx < nb_ctrl_rx))
            {
                do
                {
                    rte_pktmbuf_free(ctrl_pkts[nb_ctrl_tx]);
                } while (++nb_ctrl_tx < nb_ctrl_rx);
            }

            const uint16_t nb_rx = rte_eth_rx_burst(p->ctrl_port_id, 0, pkts,
                                                    BURST_SIZE);
            if (unlikely(nb_rx == 0))
            {
                continue;
            }

            for (int i = 0; i < nb_rx; i++)
            {
                // pkts[i]->port = ETH_PORT_ID;
                // pkts[i]->ol_flags |= PKT_TX_IPV4 | PKT_TX_IP_CKSUM | PKT_TX_TCP_CKSUM;
                // print_pkt(pkts[i]);
                rte_eth_tx_buffer(ETH_PORT_ID, 0, tx_buffer, pkts[i]);
            }
        }
    }
    return 0;
}

