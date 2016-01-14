-- These are all the currently defined config options.
-- Note; this is a fully fledged Gopher-Lua script with all libs, so you can
-- be clever, if you like.
-- Watch out for correct config names: I thought I had an auth bug
-- for ages and it was simply that I'd misnamed "SMTPPassword" as
-- "SMTPassword".
ListAddress = "some_list@host.com"  -- Should be provided for correct operation!
DeliverScript = "./default_eventloop.lua"  -- Needs to be provided in "loop" mode to handle incoming mail.
Database      = "./some_list.db"  -- Created if doesn't exist.
PollFrequency = 30  -- Seconds to wait once inbox is empty before polling again.
Constants = {SubjectTag = "[laundrylist]"}  -- Anything put in here is available in eventLoop. Only supports String->String values.
-- Account options:
IMAPHost      = "mail.1984.is"  -- Recommended!
IMAPUsername  = "some_list@host.com"  -- Some hosts use only "some_list" as username, 1984hosting.com uses full address.
IMAPPassword  = "StupidPassword1"  -- Not recommended!
IMAPPort      = 143
SMTPUsername  = IMAPUsername
SMTPPassword   = IMAPPassword
SMTPHost      = IMAPHost
SMTPPort      = 465
