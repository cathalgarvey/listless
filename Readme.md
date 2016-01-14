## Listless
**An IMAP/SMTP based, trivial discussion list**

By Cathal Garvey, Copyright 2016, Released under the GNU AGPLv3 or later.

### What's This?
I *love* discussion lists as a way to pull together communities. I used to use
Google Groups for this, back when I was naive enough to think they respected my
rights as a human being.

Since GoogleGroups and Yahoo offered such great free hosting for so long, it turns
out the ecosystem of Discussion List hosting providers is quite narrow now. Like
RSS, an anticompetitive free offering destroyed the ecosystem.

Wanting to set up discussion lists, but unwilling to host on unfriendly shores,
I was left with seemingly only one option; set up a VPS or home server and run
GNU Mailman and an entire email stack, plus a domain name to match, and deal with
all the grief that accrues to that due to misguided spam blacklisting and other
blights upon the email system.

I'd often been puzzled as to why no options exist for running Discussion lists
over IMAP/SMTP, common protocols available on almost all hosted email providers.
I can create IMAP/SMTP accounts for my domains on Shared Hosting with [1984 Hosting](https://1984hosting.com),
trivially, so if I could only use those to host discussion lists I'd have no problems.

So, I decided to write my own, and **listless** was created.

Right now, **listless** doesn't actually work. I'm working on it. The base feature
set that will come with it is:

* Configuration in lua, being as simple as a set of `Option = value` pairs, or
  as complex as a fully blown script with `os` calls to execute local commands.
* An event loop for incoming mail (over IMAP) that's scripted in Lua
    - Where the event loop receives a parsed form of incoming mail
    - Where the event loop receives the local database as an object to update or
      query.
    - Where the event loop can also execute local commands, leveraging the full
      (though hazardous!) power of lua's `io` and `os` modules.
* A local database management system that accepts Lua scripts, allowing arbitrary
  local modifications by script. Want to load in a huge CSV of subscribers? Just
  write or borrow a lua script for that. Want to fetch new subscribers from a HTTP
  resource? Shell out to wget.
* Archival storage of list traffic in a local database. At present this is dumb
  storage, however.

Later, I'd like to add:
* Limited, somewhat more secure Lua scripting over email by moderators.
* More granular access control API added to the Lua runtime for the event loop, so
  that less freeform lists can be hacked up easily.
* Search functions for the archive.
