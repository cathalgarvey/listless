-- Setup scripts have two global items set:
-- config - The Configuration file represented as a Go struct, reflectively in lua.
--           This is not, in fact, a table; tread carefully!
-- database - The boltdb database with some methods exposed to create/add/update/remove
--             entries. This is also not in fact a table. :)
-- ---
-- Creating some list subscribers is easy, but note *they are not stored yet!*
-- Also note that userMc is a MemberMeta object from Go; it is not, you guessed it,
-- a table. For its keys and methods, see `database.go`
local userMcCanPost = true
local userMcIsModerator = false
local userMc = database:CreateSubscriber("user@domain.tld",
                                          "User McMurphy",
                                          userMcCanPost,
                                          userMcIsModerator)

-- Adding subscriber to database; *now* they're stored.
database:UpdateSubscriber(userMc.Email, userMc)

-- Later, Whoops, we made a mistake, her name's not "User" it's "Usienne". Also,
-- let's make her a moderator:
local usienne = database:GetSubscriber("user@domain.tld")
usienne.Name = "Usienne McMurphy"
usienne.Moderator = true
database:UpdateSubscriber(usienne.Email, usienne)

-- Did it work?
print("Usienne is a mod:", database:IsModerator(usienne.Email)) -- "true"

-- Turns out she's sick of all the spam:
database:DelSubscriber("user@domain.tld")
