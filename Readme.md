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

Right now, **listless** barely works. I'm working on it. The base feature
set right now is:

* Configuration in lua, being as simple as a set of `Option = value` pairs, or
  as complex as a fully blown script with `os` calls to execute local commands.
  See "sample_config.lua"
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
* Pretty-ish logging with categorisation (incomplete, set `LOG` environment variable to `*` to enable, like `LOG=* listless my_conf.lua`)

### Usage / Setup

1. Get or create an email account that supports IMAP/SMTP. I use my shared hosting
   account at [1984hosting.com](https://1984hosting.com), who are awesome by the way.
2. Create a configuration file like the one in "sample_config.lua" containing your
   account details, and editing details like "SubjectTag" in the Constants table to
   your liking.
3. Create or copy/modify a lua script containing a function named `eventLoop` which
   receives as arguments `config, database, message`. The one given in `default_eventloop.lua`
   is a simple members-only mailing list which prepends message subjects with the "SubjectTag"
   string, if given. Reference this eventLoop script in your Configuration file.
   **Notice: As attractive as the idea may at times appear, do not ever write an eventLoop that
   executes remote code. Email "from" headers are trivially forged, so anyone will be able to
   execute code. At present, the lua `io`, `os` and `debug` libraries are all enabled.. Don't do it!**
4. Add some subscribers; Right now, this requires executing a lua script
   (via-mail moderator commands are [a planned feature](https://github.com/cathalgarvey/listless/issues/3)!)
    * Create a lua script similar to `sample_setup.lua`; see that file for inline
      documentation of how to use the database object to create and add subscribers.
    * Execute your file in the context of the configuration file: `./listless my_config.lua my_setup.lua`
5. Initiate the DeliveryLoop, which will iterate through incoming mail and execute `eventLoop`
   for each incoming email: `listless loop my_config.lua` (Or, if you want logs: `LOG=* loop my_config.lua`)
6. Try sending some email!

### Desired / Planned Features
* Real documentation of the Lua API.
* Limited, somewhat more secure Lua scripting over email by moderators. See [relevant issue for thoughts on "console threads" idea.](https://github.com/cathalgarvey/listless/issues/3)
* More granular access control API added to the Lua runtime for the event loop, so
  that less freeform lists can be hacked up easily.
* Search functions for the archive.
