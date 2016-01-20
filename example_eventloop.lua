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
json = require "json"
function incrementPostCount(database, username)
  -- It is not necessary to check whether something exists; it'll return an empty
  -- string if it doesn't, without changing the database. If you want to check
  -- first, you can get a table of keys with `metrics:Keys()`!
  local metrics = database:KVStore("metrics")
  local userMetricsString = metrics:Retrieve(username)
  local userMetrics = {}
  if userMetricsString ~= "" then
    userMetrics = json.decode(userMetricsString)
  else
    userMetrics = {postCount = 0, mostPosterous = false}
  end
  userMetrics.postCount = userMetrics.postCount + 1
  local newUserMetricsString, error = json.encode(userMetrics)
  if error == nil then
    -- It isn't necessary to Delete before Storing new data, as it overwritees;
    -- this is just for example!
    metrics:Delete(username)
    metrics:Store(username, newUserMetricsString)
  else
    print("Error JSON-encoding metrics object: " .. error)
  end
end

function updatePostMetrics(database)
  local metrics = database:KVStore("metrics")
  -- Update who's the most posty (pointless, costly example to demonstrate Keys method)
  -- A more sensible way would be to just iterate and get the biggest user at any
  -- given time, instead of storing that value in the database.
  local oldLeader = ""
  local newLeader = ""
  local biggest = 0
  -- Iterate over keys. This follows boltdb behaviour: keys are iterated in a
  -- consistent order which is not based on insertion; might be byte order?
  for i, k in ipairs(metrics:Keys()) do
    user = json.decode(metrics:Retrieve(k))
    if user.mostPosterous then oldLeader = k end
    if user.postCount > biggest then
      biggest = user.postCount
      newLeader = k
      print("newLeader is now:", newLeader, "with", user.postCount, "posts")
    end
  end
  if oldLeader == "" then
    oldLeader = newLeader
  end
  if newLeader ~= oldLeader then
    print("newLeader:",newLeader,"oldLeader:",oldLeader)
    -- Ignoring the error, here, but bear in mind error from the lua module
    local oldLeaderUser, err = json.decode(metrics:Retrieve(oldLeader))
    if err ~= "" then
      print("Error fetching old Leader:", err, ":", metrics:Receive(oldLeader))
    end
    local newLeaderUser, err = json.decode(metrics:Retrieve(newLeader))
    if err ~= "" then
      print("Error fetching new Leader:", err, ":", metrics:Receive(newLeader))
    end
    oldLeaderUser.mostPosterous = false
    newLeaderUser.mostPosterous = true
    -- Each call to a bucket's "store" method commits to the database.
    metrics:Store(oldLeader, json.encode(oldLeaderUser))
    metrics:Store(newLeader, json.encode(newLeaderUser))
  end
end

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
  -- You can create 'Key-Value' buckets which persist between calls, allowing
  -- you to store data, metrics, etcetera: Here's an example of usage, storing
  -- some metrics on list subscriber activity.
  -- Note: KV buckets commit themselves to the database on change, there is no
  -- need to commit a bucket manually or the database.
  incrementPostCount(database, message.Sender)
  updatePostMetrics(database)
  -- Reformat message to make it list-ey. All recipients are BCC, the "To" is
  -- the list address, From header remains intact.
  listifyMessage(config, database, message)
  -- Set the subject tag, if it's not already there:
  setSubjectTag(message, config.Constants.SubjectTag)
  -- Return message=message, send=true, error=nil
  return message, true, nil
end
