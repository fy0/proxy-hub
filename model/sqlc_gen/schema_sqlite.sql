-- 数据库建表语句
-- 生成时间: 2025-11-08 01:50:37
-- 数据库方言: sqlite
-- 总共 8 条语句


CREATE TABLE "users" ("id" text,"created_at" datetime,"updated_at" datetime,"deleted_at" datetime,"nickname" text,"avatar" text,"brief" text,"username" text NOT NULL,"password" text NOT NULL,"salt" text NOT NULL,"disabled" numeric NOT NULL DEFAULT false,PRIMARY KEY ("id"));
CREATE UNIQUE INDEX "idx_users_username" ON "users"("username");
CREATE INDEX "idx_users_deleted_at" ON "users"("deleted_at");


CREATE TABLE "user_access_tokens" ("id" text,"created_at" datetime,"updated_at" datetime,"deleted_at" datetime,"user_id" text NOT NULL,"expired_at" datetime NOT NULL,PRIMARY KEY ("id"));
CREATE INDEX "idx_access_tokens_user_id" ON "user_access_tokens"("user_id");
CREATE INDEX "idx_user_access_tokens_deleted_at" ON "user_access_tokens"("deleted_at");

