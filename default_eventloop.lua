-- This script is loaded and run fresh *every time* a message is received over IMAP.
-- This is inefficient but flexible, as it means "hot" changes can be made to the
-- event loop without downtime. It also means errors can cost you incoming messages, though.

-- For this script to work it will be loaded, and a function called "eventLoop" will
-- be called with a database table and a message table as arguments. The message table
-- must be modified as required *in place* and will be used as the outgoing message.
-- The message table is a reflective representation of this Golang struct:
-- https://godoc.org/github.com/jordan-wright/email#Email
-- The function must return the message object, a boolean indicating whether it
-- should be sent, and a string value representing any errors that may have occurred, or nil.
-- So, a successfully modified message ready for sending would be returned as (message, true, nil),
-- whereas an error would be (message, false, "Holy wtf"), and a message that triggered
-- no errors but shouldn't be sent should be returned as (message, false, nil).

function eventLoop(database, message)
  -- Message is exactly as it came in from the sender; it needs:
  -- * A list of recipients; the database table has a method "GetAllSubscribers" for this
  --   which accepts a boolean argument "moderatorsOnly"; if absent or false, all
  --   members are returned as a list-like-table, if true only members flagged as
  --   moderators (which can be nobody!) are returned.
  --local moderators = database:GetAllSubscribers(true)
  local allMembers = database:GetAllSubscribers()
  print("Hello world! Member list is: " .. table.concat(allMembers, ", "))
  message:AddRecipientList(allMembers)
  -- Testing purposes.
  message.To:append("foo@bar.org")
  -- * A reply-to header for the list
  message.SetHeader("reply-to", "mylist@tutanota.com")
  message.AddHeader("list-software", "github.com/cathalgarvey/listless")
  -- * A subject line prefix or other subject line alteration
  if message.Subject:find("[mylist]") == nil then
    message.Subject = "[mylist] " + message.Subject
  end
  return message, true, nil
end
