package com.example.twentysixx_client

/** IPv4 networks that stay on the physical interface, shared with Apple. */
internal object Ipv4Routes {
  data class Network(val start: Long, val prefix: Int) {
    val size: Long get() = 1L shl (32 - prefix)
    val address: String
      get() = listOf(24, 16, 8, 0).joinToString(".") { ((start shr it) and 255).toString() }
  }

  fun parse(text: String): List<Network> = text.lineSequence()
    .filter(String::isNotBlank)
    .map { line ->
      val pieces = line.split('/')
      require(pieces.size == 2)
      val prefix = pieces[1].toInt()
      require(prefix in 1..32)
      val octets = pieces[0].split('.').map(String::toInt)
      require(octets.size == 4 && octets.all { it in 0..255 })
      val start = octets.fold(0L) { value, octet -> (value shl 8) or octet.toLong() }
      Network(start, prefix).also { require(start % it.size == 0L) }
    }
    .toList()
    .also { require(it.isNotEmpty()) }

  /** Android 12 and earlier can include routes, so install the complement. */
  fun complement(excluded: List<Network>): List<Network> = buildList {
    fun addRange(from: Long, to: Long) {
      var address = from
      while (address < to) {
        val span = minOf(
          if (address == 0L) 1L shl 32 else java.lang.Long.lowestOneBit(address),
          java.lang.Long.highestOneBit(to - address),
        )
        add(Network(address, 32 - java.lang.Long.numberOfTrailingZeros(span)))
        address += span
      }
    }

    var cursor = 0L
    for (network in excluded.sortedBy(Network::start)) {
      addRange(cursor, network.start)
      cursor = maxOf(cursor, network.start + network.size)
    }
    addRange(cursor, 1L shl 32)
  }
}
