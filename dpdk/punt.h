#include "common.h"

struct punt_lcore_params
{
    uint16_t ctrl_port_id;
    struct rte_ring *ctrl_ring_tx;
    struct rte_ring *ctrl_ring_rx;
    struct rte_ring *punt_ring;
    struct rte_mempool *mem_pool;
};


int lcore_punt_inject(struct punt_lcore_params *lp);
