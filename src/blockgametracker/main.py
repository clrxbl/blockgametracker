#!/usr/bin/env python3
import os
import sys
import hiyapyco
import pathlib
import time
import asyncio
import ipaddress
import cachetools
import dns_cache
from aslookup import get_as_data
from loguru import logger
from func_timeout import func_set_timeout, FunctionTimedOut
from dataclasses import dataclass
from loguru_logging_intercept import setup_loguru_logging_intercept
from prometheus_client import start_http_server
from prometheus_client.core import GaugeMetricFamily, REGISTRY
from mcstatus import JavaServer, BedrockServer
from timeit import default_timer as timer
from enum import Enum

CONFIG_FILE = os.getenv("CONFIG_FILE", "kustomize/base/config/servers.yaml")
as_data_cache = cachetools.TTLCache(maxsize=256, ttl=3600)
dns_cache.override_system_resolver()

class MinecraftServerEdition(Enum):
  """
  Minecraft server edition
  """

  JAVA = str("java")
  BEDROCK = str("bedrock")


@dataclass
class MinecraftServerList:
  """
  List of Minecraft servers with name & address
  """

  edition: MinecraftServerEdition
  servers: dict = None

  def __post_init__(self):
    config = hiyapyco.load(CONFIG_FILE)
    try:
      logger.debug(self.edition.value)
      self.servers = config[f"{self.edition.value}"]
    except KeyError:
      logger.warning(f"Could not find {self.edition.value} servers in config file")
      raise


def retrieve_as_data(ip):
  """
  Retrieve AS data from cache or API
  """

  logger.debug(as_data_cache)
  for prefix in as_data_cache:
    if ipaddress.ip_address(ip) in ipaddress.ip_network(prefix):
      return as_data_cache[prefix]

  try:
    as_data = get_as_data(ip, service="cymru")
    as_data_cache[as_data.prefix] = as_data
    return as_data
  except Exception as e:
    logger.error(f"Failed to get ASN for {ip}: {e}")
    return

@dataclass
class MinecraftServer:
  """
  Class for querying a Minecraft server
  """

  edition: MinecraftServerEdition = MinecraftServerEdition.JAVA
  name: str = "Minecraft server"
  address: str = "127.0.0.1"
  port: int = None # We don't want to set a port otherwise mcstatus does not do SRV lookups
  playercount: int = None
  version: int = None
  as_number: int = 0
  as_name: str = "N/A"

  @func_set_timeout(1)
  async def query(self):
    """
    Get the number of players on the server and protocol version.
    """

    start = timer()

    if self.port is not None:
      address = f"{self.address}:{self.port}"
    else:
      address = self.address

    try:
      if self.edition == MinecraftServerEdition.JAVA:
        server = JavaServer.lookup(address)
      elif self.edition == MinecraftServerEdition.BEDROCK:
        server = BedrockServer.lookup(address)
      else:
        raise NotImplementedError("Unknown server edition")

      status = await server.async_status()
    except Exception as e:
      logger.error(f"{self} failed to query: {e}")
      await logger.complete()
      return

    try:
      ip = str(server.address.resolve_ip()).strip()
      as_data = retrieve_as_data(ip)
      logger.debug(as_data)
      self.as_number = as_data.asn
      self.as_name = as_data.as_name
    except Exception as e:
      logger.warning(f"Failed to get ASN for {self}: {e}")
      pass

    self.version = int(status.version.protocol)
    self.playercount = int(status.players.online)

    end = timer()
    logger.info(f"Queried {self.name} ({self.address}) in {round((end - start), 2)} seconds")
    return self.playercount, self.version


async def collect_metrics(sem, edition, server):
  """
  Actually collect & return the data from the server
  """
  
  async with sem:
    server = MinecraftServer(edition=edition, name=server["name"], address=server["address"])

    try:
      await server.query()
    except FunctionTimedOut:
      logger.warning(f"{server} timed out")
      pass
    
    return server


async def metric_collection(edition, servers):
  """
  Collect metrics from all servers
  """

  sem = asyncio.Semaphore(8)

  workers = [asyncio.create_task(collect_metrics(sem, edition, server)) for server in servers]
  result = await asyncio.gather(*workers)

  return result


class MinecraftCollector(object):
  """
  Collects Minecraft server metrics from a list of servers.
  """


  def collect(self):
    gauge = GaugeMetricFamily("minecraft_status_players_online_count", "Minecraft server online player counts",
            labels=["server_edition", "server_name", "server_host", "server_version", "as_number", "as_name"])

    for edition in MinecraftServerEdition:
      try:
        config = MinecraftServerList(edition)
      except Exception:
        continue
      else:
        logger.info("Collecting metrics from " + str(len(config.servers)) + " servers...")
        start = timer()

        metrics = asyncio.run(metric_collection(edition, config.servers))

        for server in metrics:
          if server.version is not None and server.playercount is not None:
            gauge.add_metric([server.edition.value, server.name, server.address, str(server.version), str(server.as_number), server.as_name], server.playercount)
          else:
            logger.warning(f"{server} did not return any metrics, not adding to gauge")
        end = timer()
        logger.info(f"Finished collecting {server.edition} metrics in {round((end - start), 2)} seconds")
    yield gauge


@logger.catch
def main(log_level):
    # Logging
    logger.remove()
    logger.add(
        sys.stderr,
        colorize=True,
        level=log_level.upper(),
        backtrace=True,
        diagnose=True,
    )

    setup_loguru_logging_intercept(
        level=log_level.upper(),
    )

    start_http_server(8080)
    REGISTRY.register(MinecraftCollector())

    while True:
        time.sleep(1)


if __name__ == "__main__":
  log_level = os.getenv("LOG_LEVEL", "INFO")
  main(log_level)
