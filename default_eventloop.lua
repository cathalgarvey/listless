-- For more documentation on how various API functions work, see example_eventloop.go
-- Full documentation will be forthcoming.

function setSubjectTag(message, tag)
  -- If a subject tag is provided as the "SubjectTag" key in the Constants table
  -- of the config file, then it is used here to add the tag to subject lines if
  -- it's not already present.
  if tag ~= "" then
    if message.Subject:find(tag, 1, true) == nil then
      message.Subject = tag .. " " .. message.Subject
    end
  end
end

function listifyMessage(config, database, message)
  message:ClearRecipients()
  message:AddToRecipient(config.ListAddress)
  message:SetHeader("reply-to", config.ListAddress)
  local allSubscribers = database:GetAllSubscribers(false)
  message:AddRecipientList(allSubscribers)
end

function eventLoop(config, database, message)
  -- Is sender an allowed poster?
  if not database:IsAllowedPost(message.Sender) then
    print("Message from address not permitted to send to list: " .. message.Sender)
    return message, false, nil
  end
  -- You can get, and set, message text using message:GetText() and message:SetText()
  print("Received message:", message.From, "-", message:GetText())
  -- Reformat message to make it list-ey. All recipients are BCC, the "To" is
  -- the list address, From header remains intact.
  listifyMessage(config, database, message)
  -- Set the subject tag, if it's not already there:
  setSubjectTag(message, config.Constants.SubjectTag)
  -- Return message=message, send=true, error=nil
  return message, true, nil
end
