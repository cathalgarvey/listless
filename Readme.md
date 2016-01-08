## Listless
**An IMAP/SMTP based, trivial discussion list**

By Cathal Garvey, Copyright 2016, Released under the GNU AGPLv3 or later.

### What's This?
I *love* discussion lists as a way to pull together communities. I used to use
Google Groups for this, back when I was naive enough to think they respected my
rights as a human being. While I participate in existing Google Groups now, I
will no longer create new enclosures for our intellectual commons that rely on
Google.

However, this leaves me with a problem, because Yahoo and Google's respective
Discussion group hosting has left a very large gap in the Discussion list
ecosystem, with the available options for hosting outside their walled gardens
being either exorbitantly expensive or public-by-default. What if I want a
private, members-only discussion list for friends or family?

I could try to host my own with Mailman, but it requires a high uptime mail server
whose IP address isn't on a spam blacklist...that's a lot of work and maintenance
for something as seemingly simple as a discussion list.

I thought it was strange that options available to me couldn't run a list using
a straightforward IMAP/SMTP account, something that anyone can get hosted by
respect-worthy services like [Tutanota](). All I really want from a Discussion
List (that is, the indispensible features) is:

* Has a membership list
* When receiving mail from anyone on this list, forwards to the whole list
* Makes optional changes to subject line and/or message body, to assist in
  group cohesion and to facilitate mail filtering by members

Wholly optionally, the following features are nice:

* Some members can issue instructions to the List via Email (mods)
* These instructions can include adding, removing, or changing the status of
  members
* Archiving

The critical list doesn't seem too difficult to implement, so I've started doing
so. My goal with `listless` is to create a list control agent that can be configured
with an IMAP/SMTP email account and a membership list, and forwards all incoming
mail after optionally changing the subject line. Everything else is optional and
will probably be implemented using a Lua scripting engine, later.

### Architecture
`listless` iterates over all incoming mail, and runs the following sequence of
events:

0. Unread mail fetched and dispatched to event loop (Thanks to [IMAPClient](github.com/tgulacsi/imapclient))
1. Mail parsed: if failure, bounce.
2. Sender identified: if member, continue. Else, bounce.
3. Message struct is passed to default or owner-provided mail handling script (lua).
    1. Can access and change subject line.
    2. Can access and change body.
    3. Can access and change member list database, which includes markup on user groups.
    4. Can decide whether to discard message or not.
    5. Can decide whether to send message to a subset of members only (i.e., mods, or reply to sender only?)
    6. Can implement, if desired, a DSL for modcommands.
4. Message passed back to Go, and reassembled.
5. Message is sent using SMTP to all recipients in message's "To" entry.

### Progress
Implemented:

* Untested event loop that fetches unread email from an IMAP account and passes it
  to a bare lua script defined by a lua config file.

Unimplemented:

* Helper functions for the Lua event script
* Database operations and lua interface thereto

Questions:

* When appending to a slice registered in lua with luar, does the original slice
  get modified *always* or only when there's no reallocation? **TODO**
