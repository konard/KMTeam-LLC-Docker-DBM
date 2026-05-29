# Docker-DBM (Docker DataBase Manager)

> **Powered by [KMTeam LLC](https://github.com/KMTeam-LLC)**

An ephemeral init container for provisioning databases in Docker Compose environments. Docker-DBM ensures that your application's database and user exist before your app container starts, with strict multi-tenant isolation for shared database servers.

---

## Table of Contents

- [Overview](#overview)
- [Infrastructure Architecture](#infrastructure-architecture)
- [Features](#features)
- [Quick Start](#quick-start)
- [Installation](#installation)
- [Configuration](#configuration)
- [Usage](#usage)
- [Security Best Practices](#security-best-practices)
- [Publishing to GHCR](#publishing-to-github-container-registry)
- [Extensibility](#extensibility)
- [Troubleshooting](#troubleshooting)
- [Contributing](#contributing)
- [License](#license)

---

## Overview

Docker-DBM is designed to run as an **ephemeral init container** within Docker Compose environments. It solves a common problem in multi-tenant database architectures:

**Problem:** When deploying applications that share a centralized database server, you need to:
1. Create a dedicated database for each application
2. Create a dedicated user with access ONLY to that database
3. Abort deployment if the database or user already exists (preventing conflicts)
4. Ensure strict isolation between tenants

**Solution:** Docker-DBM:
- ✅ Connects to your database server with admin credentials
- ✅ Checks if the requested database/user already exists
- ✅ Creates them with strict isolation if they don't exist
- ✅ Exits with code `0` on success, code `1` on failure
- ✅ Prevents your app container from starting if provisioning fails

This approach ensures that Docker Compose won't even pull your application images if database provisioning fails, saving time and preventing misconfigurations.

---

## Infrastructure Architecture

Docker-DBM acts as an ephemeral init container that provisions databases on shared database servers. The architecture uses a **layered network approach** for maximum security:

- **`db_net`** - Internal network where database servers reside (PostgreSQL, MariaDB)
- **`pg_net`** - Client network for PostgreSQL access
- **`mariadb_net`** - Client network for MariaDB access
- **Per-app internal networks** - Each application has isolated internal networking

### Network Topology Overview

```
┌─────────────────────────────────────────────────────────────────────────────────────────┐
│                                    HOST MACHINE                                         │
│                                                                                         │
│   ┌─────────────────────────────────────────────────────────────────────────────────┐   │
│   │                          db_net (Internal Database Network)                     │   │
│   │                      docker network create db_net --internal                    │   │
│   │                                                                                 │   │
│   │        ┌─────────────────────────┐     ┌─────────────────────────┐              │   │
│   │        │    PostgreSQL Server    │     │     MariaDB Server      │              │   │
│   │        │   (postgres:5432)       │     │    (mariadb:3306)       │              │   │
│   │        │                         │     │                         │              │   │
│   │        │  ┌──────────────────┐   │     │  ┌──────────────────┐   │              │   │
│   │        │  │  events_db       │   │     │  │  shop_db         │   │              │   │
│   │        │  │  analytics_db    │   │     │  │  cms_db          │   │              │   │
│   │        │  │  auth_db         │   │     │  │  inventory_db    │   │              │   │
│   │        │  └──────────────────┘   │     │  └──────────────────┘   │              │   │
│   │        └───────────┬─────────────┘     └───────────┬─────────────┘              │   │
│   │                    │                               │                            │   │
│   └────────────────────┼───────────────────────────────┼────────────────────────────┘   │
│                        │                               │                                │
│   ┌────────────────────┼───────────────┐   ┌───────────┼────────────────────────────┐   │
│   │                    │               │   │           │                            │   │
│   │          pg_net    │               │   │           │    mariadb_net             │   │
│   │    (PostgreSQL     │               │   │           │    (MariaDB Clients)       │   │
│   │     Clients)       ▼               │   │           ▼                            │   │
│   │                                    │   │                                        │   │
│   │   ┌────────────┐ ┌────────────┐    │   │    ┌────────────┐ ┌────────────┐       │   │
│   │   │  docker-   │ │  docker-   │    │   │    │  docker-   │ │  docker-   │       │   │
│   │   │  dbm       │ │  dbm       │    │   │    │  dbm       │ │  dbm       │       │   │
│   │   │ (events)   │ │(analytics) │    │   │    │  (shop)    │ │  (cms)     │       │   │
│   │   └─────┬──────┘ └─────┬──────┘    │   │    └─────┬──────┘ └─────┬──────┘       │   │
│   │         │              │           │   │          │              │              │   │
│   │         │              │           │   │          │              │              │   │
│   └─────────┼──────────────┼───────────┘   └──────────┼──────────────┼──────────────┘   │
│             │              │                          │              │                  │
│   ┌─────────┼──────────────┼────────────────────┬─────┼──────────────┼───────────────┐  │
│   │         │              │                    │     │              │               │  │
│   │         ▼              ▼                    │     ▼              ▼               │  │
│   │   ┌──────────────────────────────────┐      │ ┌──────────────────────────────┐   │  │
│   │   │        events_net                │      │ │       shop_net               │   │  │
│   │   │   (Hi.Events App Network)        │      │ │   (Shop App Network)         │   │  │
│   │   │                                  │      │ │                              │   │  │
│   │   │   ┌──────────┐   ┌──────────┐    │      │ │   ┌──────────┐  ┌──────────┐ │   │  │
│   │   │   │ all-in-  │   │  redis   │    │      │ │   │   app    │  │  cache   │ │   │  │
│   │   │   │  one     │   │          │    │      │ │   │          │  │          │ │   │  │
│   │   │   │ (pg_net) │   │(internal)│    │      │ │   │(mariadb_ │  │(internal)│ │   │  │
│   │   │   └──────────┘   └──────────┘    │      │ │   │   net)   │  └──────────┘ │   │  │
│   │   │                                  │      │ │   └──────────┘               │   │  │
│   │   └──────────────────────────────────┘      │ └──────────────────────────────┘   │  │
│   │                                             │                                    │  │
│   │            APPLICATION LAYER                │                                    │  │
│   └─────────────────────────────────────────────┴────────────────────────────────────┘  │
│                                                                                         │
└─────────────────────────────────────────────────────────────────────────────────────────┘
```

### PostgreSQL Architecture (with pg_net)

```
┌────────────────────────────────────────────────────────────────────────────────────────┐
│                                    HOST MACHINE                                        │
│                                                                                        │
│   ┌────────────────────────────────────────────────────────────────────────────────┐   │
│   │                            db_net (Internal)                                   │   │
│   │                                                                                │   │
│   │                      ┌─────────────────────────────────────┐                   │   │
│   │                      │         PostgreSQL Server           │                   │   │
│   │                      │        (postgres:5432)              │                   │   │
│   │                      │                                     │                   │   │
│   │                      │  ┌─────────────────────────────┐    │                   │   │
│   │                      │  │      events_database        │    │                   │   │
│   │                      │  │  ┌───────────────────────┐  │    │                   │   │
│   │                      │  │  │  Owner: events_user   │  │    │                   │   │
│   │                      │  │  │  REVOKE PUBLIC access │  │    │                   │   │
│   │                      │  │  └───────────────────────┘  │    │                   │   │
│   │                      │  └─────────────────────────────┘    │                   │   │
│   │                      │                                     │                   │   │
│   │                      │  ┌─────────────────────────────┐    │                   │   │
│   │                      │  │      other_database         │    │                   │   │
│   │                      │  │  (Isolated - No Access)     │    │                   │   │
│   │                      │  └─────────────────────────────┘    │                   │   │
│   │                      └───────────────┬─────────────────────┘                   │   │
│   │                                      │                                         │   │
│   └──────────────────────────────────────┼─────────────────────────────────────────┘   │
│                                          │                                             │
│   ┌──────────────────────────────────────┼─────────────────────────────────────────┐   │
│   │                       pg_net         │  (PostgreSQL Clients)                   │   │
│   │                                      │                                         │   │
│   │   ┌─────────────────┐                │                                         │   │
│   │   │  docker-dbm     │   SQL          │                                         │   │
│   │   │  (Ephemeral)    │ ──────────────►│                                         │   │
│   │   │                 │  CREATE DB     │                                         │   │
│   │   │  ┌───────────┐  │                │                                         │   │
│   │   │  │  Admin    │  │                │                                         │   │
│   │   │  │  Creds    │  │                │                                         │   │
│   │   │  │ (ADMIN_*) │  │                │                                         │   │
│   │   │  └───────────┘  │                │                                         │   │
│   │   │                 │                │                                         │   │
│   │   │  Exit 0: OK     │                │                                         │   │
│   │   │  Exit 1: Fail   │                │                                         │   │
│   │   └────────┬────────┘                │                                         │   │
│   │            │                         │                                         │   │
│   │            │ depends_on              │                                         │   │
│   │            │ service_completed_      │                                         │   │
│   │            │ successfully            │                                         │   │
│   │            ▼                         │                                         │   │
│   │   ┌─────────────────┐                │                                         │   │
│   │   │   all-in-one    │   SELECT/      │                                         │   │
│   │   │   (App)         │ ──────────────►│                                         │   │
│   │   │                 │   INSERT       │                                         │   │
│   │   │  ┌───────────┐  │                │                                         │   │
│   │   │  │   App     │  │  APP_DB_USER:APP_DB_PASS                                 │   │
│   │   │  │  Creds    │  │  (Limited to events_database only)                       │   │
│   │   │  │ (APP_DB_*)│  │                                                          │   │
│   │   │  └───────────┘  │                                                          │   │
│   │   └─────────────────┘                                                          │   │
│   │            │                                                                   │   │
│   └────────────┼───────────────────────────────────────────────────────────────────┘   │
│                │                                                                       │
│   ┌────────────┼───────────────────────────────────────────────────────────────────┐   │
│   │            │        events_net (App Internal Network)                          │   │
│   │            ▼                                                                   │   │
│   │   ┌─────────────────┐       ┌─────────────────┐                                │   │
│   │   │   all-in-one    │◄─────►│     redis       │                                │   │
│   │   │   (pg_net +     │       │  (internal      │                                │   │
│   │   │   events_net)   │       │   only)         │                                │   │
│   │   └─────────────────┘       └─────────────────┘                                │   │
│   │                                                                                │   │
│   └────────────────────────────────────────────────────────────────────────────────┘   │
│                                                                                        │
└────────────────────────────────────────────────────────────────────────────────────────┘

PostgreSQL Communication Flow:
┌──────────────┐      ┌──────────────┐      ┌──────────────────────────────────┐
│  docker-dbm  │      │   Creates    │      │           PostgreSQL             │
│   (pg_net)   │ ───► │  Database +  │ ───► │  • CREATE DATABASE events_db     │
│ ADMIN_USER   │      │    User      │      │  • CREATE USER events_user       │
│ ADMIN_PASS   │      │              │      │  • ALTER DATABASE SET OWNER      │
└──────────────┘      └──────────────┘      │  • REVOKE ALL FROM PUBLIC        │
       │                                    └──────────────────────────────────┘
       │ Exit 0
       ▼
┌──────────────┐      ┌──────────────┐      ┌──────────────────────────────────┐
│  all-in-one  │      │   Connects   │      │           PostgreSQL             │
│  (pg_net +   │ ───► │   to DB as   │ ───► │  • Full CRUD on events_database  │
│  events_net) │      │ events_user  │      │  • NO access to other databases  │
│ APP_DB_*     │      │              │      │  • NO admin privileges           │
└──────────────┘      └──────────────┘      └──────────────────────────────────┘
```

### MariaDB Architecture (with mariadb_net)

```
┌────────────────────────────────────────────────────────────────────────────────────────┐
│                                    HOST MACHINE                                        │
│                                                                                        │
│   ┌────────────────────────────────────────────────────────────────────────────────┐   │
│   │                            db_net (Internal)                                   │   │
│   │                                                                                │   │
│   │                      ┌─────────────────────────────────────┐                   │   │
│   │                      │          MariaDB Server             │                   │   │
│   │                      │        (mariadb:3306)               │                   │   │
│   │                      │                                     │                   │   │
│   │                      │  ┌─────────────────────────────┐    │                   │   │
│   │                      │  │       shop_database         │    │                   │   │
│   │                      │  │  ┌───────────────────────┐  │    │                   │   │
│   │                      │  │  │  GRANT ALL PRIVILEGES │  │    │                   │   │
│   │                      │  │  │  ON shop_database.*   │  │    │                   │   │
│   │                      │  │  │  TO 'shop_user'@'%'   │  │    │                   │   │
│   │                      │  │  └───────────────────────┘  │    │                   │   │
│   │                      │  └─────────────────────────────┘    │                   │   │
│   │                      │                                     │                   │   │
│   │                      │  ┌─────────────────────────────┐    │                   │   │
│   │                      │  │      other_database         │    │                   │   │
│   │                      │  │  (Isolated - No Access)     │    │                   │   │
│   │                      │  └─────────────────────────────┘    │                   │   │
│   │                      └───────────────┬─────────────────────┘                   │   │
│   │                                      │                                         │   │
│   └──────────────────────────────────────┼─────────────────────────────────────────┘   │
│                                          │                                             │
│   ┌──────────────────────────────────────┼─────────────────────────────────────────┐   │
│   │                   mariadb_net        │  (MariaDB Clients)                      │   │
│   │                                      │                                         │   │
│   │   ┌─────────────────┐                │                                         │   │
│   │   │  docker-dbm     │   SQL          │                                         │   │
│   │   │  (Ephemeral)    │ ──────────────►│                                         │   │
│   │   │                 │  CREATE DB     │                                         │   │
│   │   │  ┌───────────┐  │                │                                         │   │
│   │   │  │   Admin   │  │                │                                         │   │
│   │   │  │   Creds   │  │                │                                         │   │
│   │   │  │  (root)   │  │                │                                         │   │
│   │   │  └───────────┘  │                │                                         │   │
│   │   │                 │                │                                         │   │
│   │   │  Exit 0: OK     │                │                                         │   │
│   │   │  Exit 1: Fail   │                │                                         │   │
│   │   └────────┬────────┘                │                                         │   │
│   │            │                         │                                         │   │
│   │            │ depends_on              │                                         │   │
│   │            │ service_completed_      │                                         │   │
│   │            │ successfully            │                                         │   │
│   │            ▼                         │                                         │   │
│   │   ┌─────────────────┐                │                                         │   │
│   │   │    shop-app     │   SELECT/      │                                         │   │
│   │   │    (App)        │ ──────────────►│                                         │   │
│   │   │                 │   INSERT       │                                         │   │
│   │   │  ┌───────────┐  │                │                                         │   │
│   │   │  │   App     │  │  APP_DB_USER:APP_DB_PASS                                 │   │
│   │   │  │  Creds    │  │  (Limited to shop_database.* only)                       │   │
│   │   │  │ (APP_DB_*)│  │                                                          │   │
│   │   │  └───────────┘  │                                                          │   │
│   │   └─────────────────┘                                                          │   │
│   │            │                                                                   │   │
│   └────────────┼───────────────────────────────────────────────────────────────────┘   │
│                │                                                                       │
│   ┌────────────┼───────────────────────────────────────────────────────────────────┐   │
│   │            │         shop_net (App Internal Network)                           │   │
│   │            ▼                                                                   │   │
│   │   ┌─────────────────┐       ┌─────────────────┐                                │   │
│   │   │    shop-app     │◄─────►│     cache       │                                │   │
│   │   │  (mariadb_net + │       │   (internal     │                                │   │
│   │   │    shop_net)    │       │    only)        │                                │   │
│   │   └─────────────────┘       └─────────────────┘                                │   │
│   │                                                                                │   │
│   └────────────────────────────────────────────────────────────────────────────────┘   │
│                                                                                        │
└────────────────────────────────────────────────────────────────────────────────────────┘

MariaDB Communication Flow:
┌──────────────┐      ┌──────────────┐      ┌──────────────────────────────────┐
│  docker-dbm  │      │   Creates    │      │            MariaDB               │
│(mariadb_net) │ ───► │  Database +  │ ───► │  • CREATE DATABASE shop_db       │
│ ADMIN_USER   │      │    User      │      │  • CREATE USER shop_user         │
│ ADMIN_PASS   │      │              │      │  • GRANT ALL ON shop_db.*        │
└──────────────┘      └──────────────┘      │  • FLUSH PRIVILEGES              │
       │                                    └──────────────────────────────────┘
       │ Exit 0
       ▼
┌──────────────┐      ┌──────────────┐      ┌──────────────────────────────────┐
│   shop-app   │      │   Connects   │      │            MariaDB               │
│(mariadb_net +│ ───► │   to DB as   │ ───► │  • Full CRUD on shop_database    │
│  shop_net)   │      │  shop_user   │      │  • NO access to other databases  │
│ APP_DB_*     │      │              │      │  • NO admin privileges           │
└──────────────┘      └──────────────┘      └──────────────────────────────────┘
```

### Docker Network Configuration

```
Network Layer Architecture:
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

                        ┌─────────────────────────────────┐
                        │          db_net                 │
                        │    (Internal DB Network)        │
                        │                                 │
                        │  ┌──────────┐   ┌──────────┐    │
                        │  │PostgreSQL│   │ MariaDB  │    │
                        │  │  :5432   │   │  :3306   │    │
                        │  └────┬─────┘   └────┬─────┘    │
                        │       │              │          │
                        └───────┼──────────────┼──────────┘
                                │              │
           ┌────────────────────┼──────────────┼────────────────────┐
           │                    │              │                    │
           ▼                    │              │                    ▼
    ┌──────────────┐            │              │            ┌──────────────┐
    │   pg_net     │◄───────────┘              └───────────►│ mariadb_net  │
    │              │                                        │              │
    │ PostgreSQL   │                                        │  MariaDB     │
    │  Clients     │                                        │  Clients     │
    └──────┬───────┘                                        └──────┬───────┘
           │                                                       │
           │                                                       │
    ┌──────┴──────────────────────────────────────────────────────┴──────┐
    │                        APPLICATION NETWORKS                        │
    │                                                                    │
    │   ┌─────────────┐   ┌─────────────┐   ┌─────────────┐              │
    │   │ events_net  │   │  shop_net   │   │  cms_net    │   ...        │
    │   │(Hi.Events)  │   │ (Shop App)  │   │ (CMS App)   │              │
    │   └─────────────┘   └─────────────┘   └─────────────┘              │
    │                                                                    │
    └────────────────────────────────────────────────────────────────────┘

Network Creation Commands:
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

# 1. Create the internal database network (databases only)
docker network create db_net --internal

# 2. Create PostgreSQL client network
docker network create pg_net

# 3. Create MariaDB client network
docker network create mariadb_net

# 4. Connect database servers to their client networks
#    (done in docker-compose.yml for the database stack)

Network Configuration (docker-compose.yml):
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

# Database Server Stack (separate docker-compose.yml)
networks:
  db_net:
    external: true        # Internal network for DB servers
  pg_net:
    external: true        # PostgreSQL client access
  mariadb_net:
    external: true        # MariaDB client access

services:
  postgres:
    image: postgres:16
    networks:
      - db_net            # Internal DB network
      - pg_net            # Client access network

  mariadb:
    image: mariadb:11
    networks:
      - db_net            # Internal DB network
      - mariadb_net       # Client access network

# Application Stack (your app's docker-compose.yml)
networks:
  pg_net:
    external: true        # PostgreSQL access
  events_net:
    driver: bridge        # App internal network

services:
  docker-dbm:
    networks:
      - pg_net            # Only PostgreSQL access

  all-in-one:
    networks:
      - pg_net            # PostgreSQL access
      - events_net        # App internal

  redis:
    networks:
      - events_net        # App internal only (no DB access)

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
```

### Real-World Example: Hi.Events Application

```
┌─────────────────────────────────────────────────────────────────────────────────────┐
│                              Hi.Events Stack Example                                │
└─────────────────────────────────────────────────────────────────────────────────────┘

services:
  # Database provisioner - only needs pg_net
  dbm:
    image: ghcr.io/kmteam-llc/docker-dbm:latest
    networks:
      - pg_net                    # ✓ PostgreSQL access only
    env_file:
      - .env.dbm

  # Main application - needs both DB and internal
  all-in-one:
    image: daveearley/hi.events-all-in-one:latest
    depends_on:
      dbm:
        condition: service_completed_successfully
    networks:
      - pg_net                    # ✓ PostgreSQL access
      - events_net                # ✓ Internal app network
    environment:
      - DB_HOST=postgres
      - DB_PORT=5432
      - REDIS_HOST=redis

  # Redis cache - internal only
  redis:
    image: redis:7-alpine
    networks:
      - events_net                # ✓ Internal only (no DB access)

networks:
  pg_net:
    external: true                # Shared PostgreSQL network
  events_net:
    driver: bridge                # Isolated app network

┌─────────────────────────────────────────────────────────────────────────────────────┐
│                              Network Access Matrix                                  │
├───────────────┬────────────┬────────────────┬───────────────────────────────────────┤
│   Container   │   pg_net   │   events_net   │              Purpose                  │
├───────────────┼────────────┼────────────────┼───────────────────────────────────────┤
│     dbm       │     ✓      │       ✗        │ Provision DB only                     │
│  all-in-one   │     ✓      │       ✓        │ App + DB + internal services          │
│    redis      │     ✗      │       ✓        │ Cache (no external access)            │
└───────────────┴────────────┴────────────────┴───────────────────────────────────────┘
```

### Multi-Tenant Isolation with Layered Networks

```
┌─────────────────────────────────────────────────────────────────────────────────────────┐
│                         MULTI-TENANT NETWORK ISOLATION                                  │
└─────────────────────────────────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────────────────────────────────┐
│                              db_net (Internal - No Internet)                            │
│                                                                                         │
│        ┌─────────────────────────┐         ┌─────────────────────────┐                  │
│        │      PostgreSQL         │         │       MariaDB           │                  │
│        │  ┌─────────────────┐    │         │  ┌─────────────────┐    │                  │
│        │  │ events_db       │    │         │  │ shop_db         │    │                  │
│        │  │ analytics_db    │    │         │  │ cms_db          │    │                  │
│        │  └─────────────────┘    │         │  └─────────────────┘    │                  │
│        └───────────┬─────────────┘         └───────────┬─────────────┘                  │
│                    │                                   │                                │
└────────────────────┼───────────────────────────────────┼────────────────────────────────┘
                     │                                   │
    ┌────────────────┴─────────────┐     ┌───────────────┴────────────────┐
    │          pg_net              │     │         mariadb_net            │
    │                              │     │                                │
    │  ┌─────────┐   ┌─────────┐   │     │  ┌─────────┐   ┌─────────┐     │
    │  │ dbm     │   │ dbm     │   │     │  │ dbm     │   │ dbm     │     │
    │  │(events) │   │(analyt) │   │     │  │ (shop)  │   │ (cms)   │     │
    │  └────┬────┘   └────┬────┘   │     │  └────┬────┘   └────┬────┘     │
    │       │             │        │     │       │             │          │
    └───────┼─────────────┼────────┘     └───────┼─────────────┼──────────┘
            │             │                      │             │
┌───────────┼─────────────┼──────────────────────┼─────────────┼────────────────────────┐
│           │             │                      │             │                        │
│   ┌───────▼───────┐ ┌───▼───────────┐  ┌───────▼───────┐ ┌───▼───────────┐            │
│   │  events_net   │ │ analytics_net │  │   shop_net    │ │   cms_net     │            │
│   │               │ │               │  │               │ │               │            │
│   │ ┌──────────┐  │ │ ┌──────────┐  │  │ ┌──────────┐  │ │ ┌──────────┐  │            │
│   │ │all-in-one│  │ │ │  grafana │  │  │ │ shop-app │  │ │ │ cms-app  │  │            │
│   │ │(pg_net + │  │ │ │ (pg_net +│  │  │ │(mariadb_ │  │ │ │(mariadb_ │  │            │
│   │ │events_net│  │ │ │analytics_│  │  │ │net +     │  │ │ │net +     │  │            │
│   │ └──────────┘  │ │ │net)      │  │  │ │shop_net) │  │ │ │cms_net)  │  │            │
│   │               │ │ └──────────┘  │  │ └──────────┘  │ │ └──────────┘  │            │
│   │ ┌──────────┐  │ │               │  │               │ │               │            │
│   │ │  redis   │  │ │ ┌──────────┐  │  │ ┌──────────┐  │ │ ┌──────────┐  │            │
│   │ │(internal)│  │ │ │prometheus│  │  │ │  cache   │  │ │ │  search  │  │            │
│   │ └──────────┘  │ │ │(internal)│  │  │ │(internal)│  │ │ │(internal)│  │            │
│   │               │ │ └──────────┘  │  │ └──────────┘  │ │ └──────────┘  │            │
│   └───────────────┘ └───────────────┘  └───────────────┘ └───────────────┘            │
│                                                                                       │
│                        APPLICATION NETWORKS (Isolated)                                │
│                               ✗ No Cross-App Traffic                                  │
└───────────────────────────────────────────────────────────────────────────────────────┘

Security Guarantees:
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
✓ db_net is internal (--internal flag) - no internet access
✓ Database servers only on db_net + their client network (pg_net or mariadb_net)
✓ Apps only connect to the database network they need (pg_net OR mariadb_net, not both)
✓ Internal services (redis, cache) have NO database network access
✓ App networks are isolated - events_net cannot reach shop_net
✓ Lateral movement between apps is impossible via networking
✓ Database ports are NEVER exposed to host
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
```

---

## Features

- **Multi-Database Support:** PostgreSQL and MariaDB/MySQL out of the box
- **Strict Multi-Tenant Isolation:** Users can only access their assigned database
- **Interface-Driven Architecture:** Easy to extend for new database engines
- **Minimal Docker Image:** Two-stage Alpine-based build (~15MB)
- **Zero Dependencies:** Single binary with no external requirements
- **Clear Exit Codes:** Exit 0 = success, Exit 1 = failure
- **Comprehensive Logging:** Clear messages for debugging

---

## Quick Start

```bash
# 1. Clone the repository
git clone https://github.com/KMTeam-LLC/Docker-DBM.git
cd Docker-DBM

# 2. Create the required networks (one-time setup)
docker network create db_net --internal  # Internal DB network
docker network create pg_net             # PostgreSQL clients
docker network create mariadb_net        # MariaDB clients

# 3. Copy and configure environment
cp .env.example .env
# Edit .env with your database server and app credentials

# 4. Build the image
docker compose build docker-dbm

# 5. Run the provisioner
docker compose run --rm docker-dbm

# 6. On success, start your application
docker compose up -d app
```

---

## Installation

### Option 1: Use Pre-built Image from GHCR

```yaml
# docker-compose.yml
services:
  docker-dbm:
    image: ghcr.io/kmteam-llc/docker-dbm:latest
    env_file:
      - .env
    networks:
      - pg_net  # Use pg_net for PostgreSQL, mariadb_net for MariaDB

networks:
  pg_net:
    external: true
```

### Option 2: Build Locally

```yaml
# docker-compose.yml
services:
  docker-dbm:
    build:
      context: ./docker-dbm
      dockerfile: Dockerfile
    env_file:
      - .env
    networks:
      - pg_net  # Use pg_net for PostgreSQL, mariadb_net for MariaDB

networks:
  pg_net:
    external: true
```

---

## Configuration

Docker-DBM is configured entirely through environment variables.

### Environment Variables

| Variable | Description | Required | Example |
|----------|-------------|----------|---------|
| `DB_TYPE` | Database engine type | Yes | `postgres` or `mariadb` |
| `DB_HOST` | Database server hostname | Yes | `postgres` |
| `DB_PORT` | Database server port | Yes | `5432` or `3306` |
| `ADMIN_USER` | Admin username | Yes | `postgres` or `root` |
| `ADMIN_PASS` | Admin password | Yes | `secure_admin_pass` |
| `APP_DB_NAME` | Database to create | Yes | `myapp_db` |
| `APP_DB_USER` | User to create | Yes | `myapp_user` |
| `APP_DB_PASS` | User password | Yes | `secure_app_pass` |
| `DB_CONNECT_RETRIES` | Connection retry attempts | No | `5` (default) |
| `DB_CONNECT_RETRY_DELAY` | Seconds between retries | No | `2` (default) |

### Example .env File

```bash
# System Configuration
DB_TYPE=postgres

# Admin Credentials (ONLY for docker-dbm)
DB_HOST=shared_postgres
DB_PORT=5432
ADMIN_USER=postgres
ADMIN_PASS=admin_secret_password

# Application Credentials
APP_DB_NAME=myapp_database
APP_DB_USER=myapp_user
APP_DB_PASS=app_secret_password
```

---

## Usage

### Two-Step Execution Pattern

Docker-DBM is designed to be run separately from your main application deployment:

```bash
# Step 1: Provision the database (runs and exits)
docker compose run --rm docker-dbm

# Step 2: Start your application (only if Step 1 succeeds)
docker compose up -d app
```

### Automatic Dependency with Docker Compose

You can configure your app to wait for Docker-DBM using `depends_on`:

```yaml
services:
  docker-dbm:
    image: ghcr.io/kmteam-llc/docker-dbm:latest
    env_file:
      - .env
    networks:
      - pg_net              # PostgreSQL client network

  app:
    image: your-app:latest
    depends_on:
      docker-dbm:
        condition: service_completed_successfully
    environment:
      - DB_HOST=${DB_HOST}
      - DB_PORT=${DB_PORT}
      - DB_NAME=${APP_DB_NAME}
      - DB_USER=${APP_DB_USER}
      - DB_PASS=${APP_DB_PASS}
    networks:
      - pg_net              # Database access
      - app_internal        # Internal services

  redis:
    image: redis:7-alpine
    networks:
      - app_internal        # Internal only - NO database access

networks:
  pg_net:
    external: true
  app_internal:
    driver: bridge
```

### Exit Codes

| Code | Meaning |
|------|---------|
| `0` | Database and user created successfully |
| `1` | Error: database exists, user exists, or provisioning failed |

---

## Security Best Practices

When running a centralized database server hosting multiple applications, security is critical. Docker-DBM implements and recommends the following security principles:

### 1. Strict Database Isolation (Multi-Tenancy)

Docker-DBM configures users with **full CRUD access ONLY on their assigned database**:

- **PostgreSQL:** Creates the database with the user as owner, then executes `REVOKE ALL ON DATABASE dbname FROM PUBLIC` to prevent other users from even connecting.
- **MariaDB:** Grants `ALL PRIVILEGES` scoped strictly to `database_name.*`, ensuring no access to other databases.

This ensures complete tenant isolation - users cannot see, query, or modify any database except their own.

### 2. Layered Network Isolation

Docker-DBM recommends a **layered network architecture** for maximum security:

```yaml
networks:
  # Internal database network (no internet access)
  db_net:
    external: true
    # Created with: docker network create db_net --internal
  
  # PostgreSQL client network
  pg_net:
    external: true
  
  # MariaDB client network
  mariadb_net:
    external: true
  
  # App-specific internal network
  events_net:
    driver: bridge
```

**Network Assignment by Service Type:**

| Service Type | Networks | Purpose |
|--------------|----------|---------|
| Database Servers | `db_net` + `pg_net` or `mariadb_net` | Internal + client access |
| docker-dbm | `pg_net` or `mariadb_net` | DB provisioning only |
| App (needs DB) | `pg_net`/`mariadb_net` + `app_net` | DB + internal services |
| Internal services | `app_net` only | No external access |

**Example (Hi.Events):**
```yaml
services:
  dbm:
    networks: [pg_net]           # DB provisioning only
  
  all-in-one:
    networks: [pg_net, events_net]  # DB + app internal
  
  redis:
    networks: [events_net]       # Internal only (no DB)
```

This prevents lateral movement between applications and ensures internal services (like Redis) have no database network access.

### 3. Credential Scoping

```yaml
services:
  docker-dbm:
    # Has access to ADMIN credentials
    env_file:
      - .env  # Contains ADMIN_USER, ADMIN_PASS

  app:
    # ONLY has access to APP credentials
    environment:
      - DB_USER=${APP_DB_USER}
      - DB_PASS=${APP_DB_PASS}
      # Note: NO ADMIN credentials here!
```

**Critical:** `ADMIN_PASS` must **only** be exposed to the ephemeral `docker-dbm` container and **NEVER** mapped to your actual application containers.

### 4. Port Hiding

**Never expose database ports to the host machine:**

```yaml
# ❌ DON'T DO THIS
services:
  postgres:
    ports:
      - "5432:5432"  # Exposes database to host network

# ✅ DO THIS INSTEAD
services:
  postgres:
    # No ports section - only accessible via Docker network
    networks:
      - db_net      # Internal database network
      - pg_net      # Client access network
```

Rely on Docker's internal DNS (`DB_HOST=postgres`) instead of exposing ports. This keeps your database invisible to the host machine and external networks.

---

## Publishing to GitHub Container Registry

### Automated Publishing (GitHub Actions)

Docker-DBM ships with a GitHub Actions workflow that builds the image and
publishes it to GHCR automatically. See
[`.github/workflows/docker-publish.yml`](.github/workflows/docker-publish.yml).

The workflow runs when:

- A semantic-version tag (e.g. `v1.0.0`) is pushed.
- A GitHub Release is published.
- It is triggered manually from the **Actions** tab (`workflow_dispatch`).

It uses [`docker/build-push-action`](https://github.com/docker/build-push-action)
together with [`docker/metadata-action`](https://github.com/docker/metadata-action)
to push the following tags to `ghcr.io/kmteam-llc/docker-dbm`:

| Trigger        | Tags produced                                  |
| -------------- | ---------------------------------------------- |
| Tag `v1.2.3`   | `v1.2.3`, `1.2.3`, `1.2`, `1`, `latest`        |
| Pre-release    | `v1.2.3-rc.1`, `1.2.3-rc.1` (no `latest`)      |
| Manual dispatch| `latest`                                       |

Authentication uses the built-in `GITHUB_TOKEN`, so **no extra secrets are
required**. To cut a release, push a tag:

```bash
git tag v1.0.0
git push origin v1.0.0
```

### Manual Publishing

To publish Docker-DBM to GitHub Container Registry (GHCR) manually:

```bash
# 1. Login to GHCR
echo $GITHUB_TOKEN | docker login ghcr.io -u USERNAME --password-stdin

# 2. Build the image
cd docker-dbm
docker build -t ghcr.io/kmteam-llc/docker-dbm:latest .

# 3. Tag with version (optional but recommended)
docker tag ghcr.io/kmteam-llc/docker-dbm:latest ghcr.io/kmteam-llc/docker-dbm:v1.0.0

# 4. Push to GHCR
docker push ghcr.io/kmteam-llc/docker-dbm:latest
docker push ghcr.io/kmteam-llc/docker-dbm:v1.0.0
```

### Setting Package Visibility

After pushing, set the package visibility in GitHub:
1. Go to your GitHub profile → Packages
2. Find `docker-dbm`
3. Package Settings → Change visibility → Public

---

## Extensibility

Docker-DBM uses an **interface-driven design** to support multiple database engines:

```go
// provisioner/provisioner.go
type DatabaseProvisioner interface {
    Provision(config Config) error
    Name() string
}
```

### Adding a New Database Engine

To add support for a new database (e.g., MongoDB):

1. Create `provisioner/mongodb.go`
2. Implement the `DatabaseProvisioner` interface
3. Register in `GetProvisioner()` function

```go
// provisioner/mongodb.go
type MongoDBProvisioner struct{}

func (m *MongoDBProvisioner) Name() string {
    return "mongodb"
}

func (m *MongoDBProvisioner) Provision(config Config) error {
    // Implementation here
}
```

Community contributions for new database engines are welcome!

---

## Project Structure

```
Docker-DBM/
├── docker-dbm/
│   ├── main.go                    # Entry point
│   ├── provisioner/
│   │   ├── provisioner.go         # Interface definitions
│   │   ├── postgres.go            # PostgreSQL implementation
│   │   └── mariadb.go             # MariaDB implementation
│   ├── Dockerfile                 # Multi-stage build
│   ├── go.mod
│   └── go.sum
├── docker-compose.yml             # Template for users
├── .env.example                   # Environment template
└── README.md                      # This file
```

---

## Troubleshooting

### Database or User Already Exists

If Docker-DBM fails with "database already exists" or "user already exists", this means the target resources were previously created. This is **intentional** fail-closed behavior to prevent conflicts.

**Common causes:**
- A previous deployment already provisioned these resources
- A partial provisioning occurred (see edge-case below)
- Multiple projects are trying to use the same database/user name

**Solution:** If you need to re-provision, manually clean up on the database server:

```sql
-- PostgreSQL
DROP DATABASE IF EXISTS myapp_database;
DROP USER IF EXISTS myapp_user;

-- MariaDB
DROP DATABASE IF EXISTS myapp_database;
DROP USER IF EXISTS 'myapp_user'@'%';
```

### Partial Provisioning Edge-Case

In rare circumstances (e.g., container crash between `CREATE DATABASE` and `CREATE USER`), you may have an orphaned database or user. If the next run fails with "Database already exists" but the user doesn't exist (or vice versa), manually clean up the orphaned resource using the SQL commands above.

### Connection Refused on First Run

If Docker-DBM fails immediately with "connection refused", the database server may still be initializing. Docker-DBM includes automatic retry logic (5 attempts with 2-second delays by default).

**To customize retry behavior:**
```yaml
services:
  docker-dbm:
    environment:
      - DB_CONNECT_RETRIES=10       # Number of retry attempts (default: 5)
      - DB_CONNECT_RETRY_DELAY=3    # Seconds between retries (default: 2)
```

---

## Contributing

We welcome contributions! Please see our contribution guidelines:

1. Fork the repository
2. Create a feature branch (`git checkout -b feature/mongodb-support`)
3. Commit your changes (`git commit -am 'Add MongoDB support'`)
4. Push to the branch (`git push origin feature/mongodb-support`)
5. Open a Pull Request

### Areas for Contribution

- **New Database Engines:** MongoDB, Redis, CockroachDB, etc.
- **Additional Configuration Options:** SSL/TLS support, connection pooling
- **Testing:** Unit tests, integration tests
- **Documentation:** Translations, tutorials

---

## License

This project is open source and available under the [AGPL v3 License](LICENSE).

---

## Support

- **Issues:** [GitHub Issues](https://github.com/KMTeam-LLC/Docker-DBM/issues)
- **Discussions:** [GitHub Discussions](https://github.com/KMTeam-LLC/Docker-DBM/discussions)

---

**Made with ❤️ by [KMTeam LLC](https://github.com/KMTeam-LLC)**

