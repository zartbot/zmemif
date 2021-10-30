#include "common.h"

struct io_lcore_params
{
    struct rte_ring *ctrl_ring_tx;
    struct rte_ring *ctrl_ring_rx;
    struct rte_ring *punt_ring;
    struct rte_mempool *mem_pool;
    uint16_t ctrl_port_id;
    uint16_t memif_start_idx;
    uint16_t num_memif_per_core;
    uint16_t core_id;
    
};

int lcore_io_rx_memif(struct io_lcore_params *p);
int lcore_io_rx(struct io_lcore_params *p);
int lcore_io_tx(struct io_lcore_params *p);
