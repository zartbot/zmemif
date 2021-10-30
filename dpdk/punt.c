#include "punt.h"
#include <sys/socket.h>
#include <arpa/inet.h>
#include <netinet/in.h>

#define PORT 8080
#define MAXLINE 10000

/* Punt Header

 protocol "DP ID:8, PortID:16, Cause: 8,Timestamp: 64"

 0                   1                   2                   3  
 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|     DP ID     |             PortID            |      Cause    |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|                                                               |
+                           Timestamp                           +
|                                                               |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
*/
//Defined header in UDP
struct ruta_punt_hdr
{
    uint8_t dp_id;
    uint16_t port_id;
    uint8_t cause;
    uint64_t timestamp;
} __attribute__((packed));



//TODO: defined a control protocol here to update FIB ....
struct ruta_inject_hdr
{
    uint8_t dp_id;
    uint16_t port_id;
    uint8_t cause;
    uint64_t timestamp;
} __attribute__((packed));



int lcore_punt_inject(struct punt_lcore_params *lp)
{
    struct rte_eth_dev_tx_buffer *buffer;

    int sockfd;
    char msg[MAXLINE];
    struct sockaddr_in servaddr;
    memset(&servaddr, 0, sizeof(servaddr));

    //Punt udp socket
    servaddr.sin_family = AF_INET;
    servaddr.sin_port = htons(PORT);
    servaddr.sin_addr.s_addr = inet_addr("127.0.0.1"); //INADDR_ANY;
    sockfd = socket(AF_INET, SOCK_DGRAM, 0);

    const int socket_id = rte_socket_id();
    printf("Core %u doing RX dequeue.\n", rte_lcore_id());

    struct rte_mbuf *ctrl_bufs[BURST_SIZE_TX];
    struct rte_mbuf *inject_bufs[BURST_SIZE];
    struct rte_mbuf *punt_bufs[BURST_SIZE];
    struct ruta_punt_hdr *punt_hdr;

    while (!force_quit)
    {
        /* ctrl_ring_rx -> ctrl_port */ 
        const uint16_t nb_rx1 = rte_ring_dequeue_burst(lp->ctrl_ring_rx,
                                                       (void *)ctrl_bufs, BURST_SIZE_TX, NULL);

        unsigned int nb_tx1 = rte_eth_tx_burst(lp->ctrl_port_id, 0, ctrl_bufs, nb_rx1);
        if (unlikely(nb_tx1 < nb_rx1))
        {
            do
            {
                rte_pktmbuf_free(ctrl_bufs[nb_tx1]);
            } while (++nb_tx1 < nb_rx1);
        }


        /* ctrl_port -> ctrl_ring_tx */ 
        const uint16_t nb_rx2 = rte_eth_rx_burst(lp->ctrl_port_id, 0, inject_bufs,
                                                 BURST_SIZE);
        for (int i = 0; i < nb_rx2; i++)
        {
            //TODO: Destination lookup or src lookup
            inject_bufs[i]->port = 0;
            inject_bufs[i]->ol_flags |= PKT_TX_IPV4 | PKT_TX_IP_CKSUM | PKT_TX_UDP_CKSUM;
            rte_ring_enqueue(lp->ctrl_ring_tx, inject_bufs[i]);
        }

        /* punt_ring -> udp socket to host control plane */ 
        const uint16_t nb_rx3 = rte_ring_dequeue_burst(lp->punt_ring,
                                                       (void *)punt_bufs, BURST_SIZE, NULL);
        if (unlikely(nb_rx3 == 0))
        {
            continue;
        }
        for (int i = 0; i < nb_rx3; i++)
        {

            void *pkt_ptr;
            pkt_ptr = (void *)(rte_pktmbuf_mtod(punt_bufs[i], char *));
            //TODO: Add punt header
            memcpy(msg+sizeof(struct ruta_punt_hdr), pkt_ptr, rte_pktmbuf_data_len(punt_bufs[i]));

            punt_hdr = (struct ruta_punt_hdr *)msg;
            punt_hdr->dp_id =0;
            punt_hdr->port_id = punt_bufs[i]->port;
            
            sendto(sockfd, msg, rte_pktmbuf_data_len(punt_bufs[i]),
                   MSG_CONFIRM, (const struct sockaddr *)&servaddr,
                   sizeof(servaddr));

            rte_pktmbuf_free(punt_bufs[i]);
        }
        //TODO: udp ctrl socket inject by golang ?
        // Need to implement some complex logic here as forwarding mgr
    }
    return 0;
}
