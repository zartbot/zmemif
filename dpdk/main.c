#include <stdint.h>
#include <unistd.h>
#include <inttypes.h>

#include <pthread.h>
#include <string.h>

#include <sys/socket.h>
#include <sys/ioctl.h>
#include <netinet/in.h>
#include <net/if.h>
#include <net/if_arp.h>
#include <arpa/inet.h>

#include "portinit.h"
#include "io.h"
#include "punt.h"

int main(int argc, char *argv[])
{
    struct rte_mempool *mbuf_pool;

    // TODO: Add CLI parser
    unsigned int first_lcore = 25;
    char *ipaddr = "11.11.11.11";
    char *netmask = "255.255.255.0";
    int num_memif = 16;
    int num_rx_core = 4;
    int num_tx_core = 2;
    int server_mode = 1;
    
    if (num_memif % num_rx_core != 0)
    {
        rte_exit(EXIT_FAILURE, "mismatch config on RX core number and total memif number");
    }

    if (num_memif % num_tx_core != 0)
    {
        rte_exit(EXIT_FAILURE, "mismatch config on TX core number and total memif number");
    }
    int num_memif_per_rx = num_memif / num_rx_core;
    int num_memif_per_tx = num_memif / num_tx_core;

    struct rte_ring *ctrl_ring_rx, *ctrl_ring_tx;
    struct rte_ring *punt_ring;

    struct io_lcore_params lp_tx[MAX_SERVICE_CORE];
    struct io_lcore_params lp_rx[MAX_SERVICE_CORE];
    struct punt_lcore_params *lp_punt;

    /* single-file-segment is only used for memif client mode.*/
    char *dpdk_single_file_segment = "--single-file-segments";
    char **new_argv = malloc((argc + 2) * sizeof(*dpdk_single_file_segment));
    memmove(new_argv, argv, sizeof(*dpdk_single_file_segment) * argc);
    new_argv[argc] = dpdk_single_file_segment;
    new_argv[argc + 1] = 0;

    if (server_mode == 0) {
        argc++;
        argv = new_argv;
    }


    int retval = rte_eal_init(argc, argv);
    if (retval < 0)
        rte_exit(EXIT_FAILURE, "initialize fail!");

    free(new_argv);

    /* Creates a new mempool in memory to hold the mbufs. */
    mbuf_pool = rte_pktmbuf_pool_create("MBUF_POOL", NUM_MBUFS * 4,
                                        MBUF_CACHE_SIZE, 0,
                                        RTE_MBUF_DEFAULT_BUF_SIZE, rte_socket_id());
    if (mbuf_pool == NULL)
        rte_exit(EXIT_FAILURE, "Cannot create mbuf pool\n");

    /* Create Ctrl queue */
    ctrl_ring_rx = rte_ring_create("ctrl_ring_rx", SCHED_RX_RING_SZ,
                                   rte_socket_id(), RING_F_MC_HTS_DEQ | RING_F_MP_HTS_ENQ);
    if (ctrl_ring_rx == NULL)
        rte_exit(EXIT_FAILURE, "Cannot create ctrl rx ring\n");

    ctrl_ring_tx = rte_ring_create("ctrl_ring_tx", SCHED_TX_RING_SZ,
                                   rte_socket_id(), RING_F_MC_HTS_DEQ | RING_F_MP_HTS_ENQ);
    if (ctrl_ring_tx == NULL)
        rte_exit(EXIT_FAILURE, "Cannot create ctrl tx ring\n");

    punt_ring = rte_ring_create("punt_ring", SCHED_RX_RING_SZ,
                                rte_socket_id(), RING_F_MC_HTS_DEQ | RING_F_MP_HTS_ENQ);
    if (punt_ring == NULL)
        rte_exit(EXIT_FAILURE, "Cannot create punt ring\n");

    uint16_t ctrl_port_id = rte_eth_dev_count_total();
    

    struct rte_ether_addr eth_mac_addr;
    retval = rte_eth_macaddr_get(ETH_PORT_ID, &eth_mac_addr);

    /* Create control virtual port */
    char vhost_user_cfg[255];
    sprintf(vhost_user_cfg, "iface=rutasys0,path=/dev/vhost-net,queues=1,queue_size=1024,mac=%02" PRIx8 ":%02" PRIx8 ":%02" PRIx8 ":%02" PRIx8 ":%02" PRIx8 ":%02" PRIx8 "\n",
            eth_mac_addr.addr_bytes[0], eth_mac_addr.addr_bytes[1],
            eth_mac_addr.addr_bytes[2], eth_mac_addr.addr_bytes[3],
            eth_mac_addr.addr_bytes[4], eth_mac_addr.addr_bytes[5]);

    //printf("%s\n", vhost_user_cfg);
    rte_vdev_init("virtio_user0", vhost_user_cfg);

    if (port_init(ctrl_port_id, mbuf_pool, 1, 1) != 0)
        rte_exit(EXIT_FAILURE, "Cannot init port %" PRIu16 "\n", ctrl_port_id);


    /* Memif for golang */
    uint16_t memif_start_idx = ctrl_port_id + 1;
    for (int memif_idx = 0; memif_idx < num_memif; memif_idx++)
    {
        char if_name[12];
        sprintf(if_name, "net_memif%d", memif_idx);
        char params[88];
	if (server_mode == 1) {
           sprintf(params, "role=server,socket=/tmp/memif.sock,socket-abstract=no,rsize=10,id=%d", memif_idx);
	} else {
           sprintf(params, "role=client,socket=/tmp/memif.sock,zero-copy=yes,socket-abstract=no,rsize=10,id=%d", memif_idx);
	}
        retval = rte_vdev_init(if_name, params);
        if (retval < 0)
        {
            rte_exit(EXIT_FAILURE, "initialize memif failed!");
        }
        port_init(memif_start_idx + memif_idx , mbuf_pool, 1, 1);
    }

   
    /* Initialize eth port */
    if (port_init(ETH_PORT_ID, mbuf_pool, num_rx_core, num_rx_core) != 0)
        rte_exit(EXIT_FAILURE, "Cannot init port %" PRIu16 "\n", ETH_PORT_ID);
    rte_eth_dev_default_mac_addr_set(ETH_PORT_ID, &eth_mac_addr);


    // Config tap0 interface address and mac address
    struct ifreq ifr;
    const char *name = "rutasys0";
    int fd = socket(PF_INET, SOCK_DGRAM, IPPROTO_IP);

    strncpy(ifr.ifr_name, name, IFNAMSIZ);

    ifr.ifr_addr.sa_family = AF_INET;
    inet_pton(AF_INET, ipaddr, ifr.ifr_addr.sa_data + 2);
    ioctl(fd, SIOCSIFADDR, &ifr);

    inet_pton(AF_INET, netmask, ifr.ifr_addr.sa_data + 2);
    ioctl(fd, SIOCSIFNETMASK, &ifr);

    ifr.ifr_addr.sa_family = ARPHRD_ETHER;
    for (int i = 0; i < 6; ++i)
        ifr.ifr_hwaddr.sa_data[i] = eth_mac_addr.addr_bytes[i];
    ioctl(fd, SIOCSIFHWADDR, &ifr);

    ioctl(fd, SIOCGIFFLAGS, &ifr);
    strncpy(ifr.ifr_name, name, IFNAMSIZ);
    ifr.ifr_flags |= (IFF_UP | IFF_RUNNING);
    ioctl(fd, SIOCSIFFLAGS, &ifr);

    printf("system init finished... starting service process...\n");

    unsigned int lcore_num = first_lcore;
    /* Start IO-RX process */

    
    for (int i = 0; i < num_rx_core; ++i)
    {
        lp_rx[i].ctrl_ring_rx = ctrl_ring_rx;
        lp_rx[i].punt_ring = punt_ring;
        lp_rx[i].mem_pool = mbuf_pool;
        lp_rx[i].ctrl_port_id = ctrl_port_id;
        lp_rx[i].memif_start_idx = i * num_memif_per_rx+memif_start_idx;
        lp_rx[i].num_memif_per_core = num_memif_per_rx;
        lp_rx[i].core_id = i;

        rte_eal_remote_launch((lcore_function_t *)lcore_io_rx_memif, &lp_rx[i], lcore_num++);
    }

    /*  Start IO-TX process */
    for (int i = 0; i < num_tx_core; ++i)
    {
        lp_tx[i].ctrl_ring_rx = ctrl_ring_rx;
        lp_tx[i].ctrl_ring_tx = ctrl_ring_tx;
        lp_tx[i].punt_ring = punt_ring;
        lp_tx[i].mem_pool = mbuf_pool;
        lp_tx[i].ctrl_port_id = ctrl_port_id;
        lp_tx[i].core_id = i;
        lp_tx[i].memif_start_idx = i * num_memif_per_tx+memif_start_idx;
        lp_tx[i].num_memif_per_core = num_memif_per_tx;

        rte_eal_remote_launch((lcore_function_t *)lcore_io_tx, &lp_tx[i], lcore_num++);
    }

    rte_eal_wait_lcore(first_lcore);
    return 0;
}

