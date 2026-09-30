#include <boost/crc.hpp>
#include <boost/lockfree/spsc_queue.hpp>

int main() {
    boost::crc_32_type crc;
    const char data[] = "lx06";
    crc.process_bytes(data, sizeof(data) - 1);

    boost::lockfree::spsc_queue<unsigned char> queue(4);
    return queue.push(static_cast<unsigned char>(crc.checksum())) ? 0 : 1;
}
