-- This script is loaded and run fresh *every time* a message is received over IMAP.
-- This is inefficient but flexible, as it means "hot" changes can be made to the
-- event loop without downtime. It also means errors can cost you incoming messages, though.
-- For this script to work it will be loaded, and a function called "eventLoop" will
-- be called with the config table, the database table and a message table as arguments.
-- The message table must be modified as required *in place* and will be used as
-- the outgoing message.
-- The message table is a wrapper over:
-- https://godoc.org/github.com/jordan-wright/email#Email
-- ..with some extra convenience methods and extra logic to handle recipients.
-- If any additional data is desired in the eventLoop that could be set at Config-time,
-- the config option "Constants" can be a string->string table which is exposed
-- in the eventLoop function as config.Constants. This allows the authorship of
-- generic eventLoop functions like the below which can behave similarly between
-- different lists.
-- The eventLoop function must return the message object, a boolean indicating
-- whether it should be sent, and a string value representing any errors that
-- may have occurred, or nil.
-- So, a successfully modified message ready for sending would be returned as (message, true, nil),
-- whereas an error would be (message, false, "Holy wtf"), and a message that triggered
-- no errors but shouldn't be sent should be returned as (message, false, nil).
function eventLoop(config, database, message)
  -- Is sender an allowed poster?
  if not database:IsAllowedPost(message.Sender) then
    print("Message from address not permitted to send to list: " .. message.Sender)
    return message, false, nil
  end
  -- Reformat message to make it list-ey. All recipients are BCC, the "To" is
  -- the list address, From header remains intact.
  message:ClearRecipients()
  message:AddToRecipient(config.ListAddress)
  message:SetHeader("reply-to", config.ListAddress)
  local allSubscribers = database:GetAllSubscribers(false)
  message:AddRecipientList(allSubscribers)
  -- If a subject tag is provided as the "SubjectTag" key in the Constants table
  -- of the config file, then it is used here to add the tag to subject lines if
  -- it's not already present.
  if config.Constants.SubjectTag ~= "" then
    if message.Subject:find(config.Constants.SubjectTag, 1, true) == nil then
      message.Subject = config.Constants.SubjectTag .. " " .. message.Subject
    end
  end
  -- Return message=message, send=true, error=nil
  return message, true, nil
end
