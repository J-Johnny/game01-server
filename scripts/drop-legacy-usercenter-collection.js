// One-time cleanup for the pre-domain UserCenter schema.
// Run this script explicitly with mongosh after checking the target database.

const collectionName = "user_center";
const result = db.getCollection(collectionName).drop();

printjson({
  database: db.getName(),
  collection: collectionName,
  dropped: result,
});
