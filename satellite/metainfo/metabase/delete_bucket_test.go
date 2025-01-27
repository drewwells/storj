// Copyright (C) 2020 Storj Labs, Inc.
// See LICENSE for copying information.

package metabase_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"storj.io/common/storj"
	"storj.io/common/testcontext"
	"storj.io/common/uuid"
	"storj.io/storj/satellite/metainfo/metabase"
)

func TestDeleteBucketObjects(t *testing.T) {
	All(t, func(ctx *testcontext.Context, t *testing.T, db *metabase.DB) {
		obj1 := randObjectStream()
		obj2 := randObjectStream()
		obj3 := randObjectStream()
		objX := randObjectStream()
		objY := randObjectStream()

		obj2.ProjectID, obj2.BucketName = obj1.ProjectID, obj1.BucketName
		obj3.ProjectID, obj3.BucketName = obj1.ProjectID, obj1.BucketName
		objX.ProjectID = obj1.ProjectID
		objY.BucketName = obj1.BucketName

		t.Run("invalid options", func(t *testing.T) {
			defer DeleteAll{}.Check(ctx, t, db)

			DeleteBucketObjects{
				Opts: metabase.DeleteBucketObjects{
					Bucket: metabase.BucketLocation{
						ProjectID:  uuid.UUID{},
						BucketName: "",
					},
				},
				ErrClass: &metabase.ErrInvalidRequest,
				ErrText:  "ProjectID missing",
			}.Check(ctx, t, db)

			DeleteBucketObjects{
				Opts: metabase.DeleteBucketObjects{
					Bucket: metabase.BucketLocation{
						ProjectID:  uuid.UUID{1},
						BucketName: "",
					},
				},
				ErrClass: &metabase.ErrInvalidRequest,
				ErrText:  "BucketName missing",
			}.Check(ctx, t, db)

			Verify{}.Check(ctx, t, db)
		})

		t.Run("empty bucket", func(t *testing.T) {
			defer DeleteAll{}.Check(ctx, t, db)

			DeleteBucketObjects{
				Opts: metabase.DeleteBucketObjects{
					Bucket: obj1.Location().Bucket(),
					DeletePieces: func(ctx context.Context, segments []metabase.DeletedSegmentInfo) error {
						return errors.New("shouldn't be called")
					},
				},
				Deleted: 0,
			}.Check(ctx, t, db)

			Verify{}.Check(ctx, t, db)
		})

		t.Run("one object", func(t *testing.T) {
			defer DeleteAll{}.Check(ctx, t, db)

			createObject(ctx, t, db, obj1, 2)

			DeleteBucketObjects{
				Opts: metabase.DeleteBucketObjects{
					Bucket: obj1.Location().Bucket(),
					DeletePieces: func(ctx context.Context, segments []metabase.DeletedSegmentInfo) error {
						if len(segments) != 2 {
							return errors.New("expected 2 segments")
						}
						for _, s := range segments {
							if len(s.Pieces) != 1 {
								return errors.New("expected 1 piece per segment")
							}
						}
						return nil
					},
				},
				Deleted: 1,
			}.Check(ctx, t, db)

			Verify{}.Check(ctx, t, db)
		})

		t.Run("empty object", func(t *testing.T) {
			defer DeleteAll{}.Check(ctx, t, db)

			createObject(ctx, t, db, obj1, 0)

			DeleteBucketObjects{
				Opts: metabase.DeleteBucketObjects{
					Bucket: obj1.Location().Bucket(),
					DeletePieces: func(ctx context.Context, segments []metabase.DeletedSegmentInfo) error {
						return errors.New("expected no segments")
					},
				},
				// TODO: fix the count for objects without segments
				// this should be 1.
				Deleted: 0,
			}.Check(ctx, t, db)

			Verify{}.Check(ctx, t, db)
		})

		t.Run("three objects", func(t *testing.T) {
			defer DeleteAll{}.Check(ctx, t, db)

			createObject(ctx, t, db, obj1, 2)
			createObject(ctx, t, db, obj2, 2)
			createObject(ctx, t, db, obj3, 2)

			DeleteBucketObjects{
				Opts: metabase.DeleteBucketObjects{
					Bucket:    obj1.Location().Bucket(),
					BatchSize: 2,
					DeletePieces: func(ctx context.Context, segments []metabase.DeletedSegmentInfo) error {
						if len(segments) != 2 && len(segments) != 4 {
							return errors.New("expected 2 or 4 segments")
						}
						for _, s := range segments {
							if len(s.Pieces) != 1 {
								return errors.New("expected 1 piece per segment")
							}
						}
						return nil
					},
				},
				Deleted: 3,
			}.Check(ctx, t, db)

			Verify{}.Check(ctx, t, db)
		})

		t.Run("don't delete non-exact match", func(t *testing.T) {
			defer DeleteAll{}.Check(ctx, t, db)

			createObject(ctx, t, db, obj1, 1)
			createObject(ctx, t, db, objX, 1)
			createObject(ctx, t, db, objY, 1)
			now := time.Now()

			DeleteBucketObjects{
				Opts: metabase.DeleteBucketObjects{
					Bucket: obj1.Location().Bucket(),
				},
				Deleted: 1,
			}.Check(ctx, t, db)

			Verify{
				Objects: []metabase.RawObject{
					{
						ObjectStream: objX,
						CreatedAt:    now,
						Status:       metabase.Committed,
						SegmentCount: 1,

						TotalPlainSize:     512,
						TotalEncryptedSize: 1024,
						FixedSegmentSize:   512,
						Encryption:         defaultTestEncryption,
					},
					{
						ObjectStream: objY,
						CreatedAt:    now,
						Status:       metabase.Committed,
						SegmentCount: 1,

						TotalPlainSize:     512,
						TotalEncryptedSize: 1024,
						FixedSegmentSize:   512,
						Encryption:         defaultTestEncryption,
					},
				},
				Segments: []metabase.RawSegment{
					{
						StreamID:  objX.StreamID,
						Position:  metabase.SegmentPosition{Part: 0, Index: 0},
						CreatedAt: &now,

						RootPieceID:       storj.PieceID{1},
						Pieces:            metabase.Pieces{{Number: 0, StorageNode: storj.NodeID{2}}},
						EncryptedKey:      []byte{3},
						EncryptedKeyNonce: []byte{4},
						EncryptedETag:     []byte{5},

						EncryptedSize: 1024,
						PlainSize:     512,
						PlainOffset:   0,

						Redundancy: defaultTestRedundancy,
					},
					{
						StreamID:  objY.StreamID,
						Position:  metabase.SegmentPosition{Part: 0, Index: 0},
						CreatedAt: &now,

						RootPieceID:       storj.PieceID{1},
						Pieces:            metabase.Pieces{{Number: 0, StorageNode: storj.NodeID{2}}},
						EncryptedKey:      []byte{3},
						EncryptedKeyNonce: []byte{4},
						EncryptedETag:     []byte{5},

						EncryptedSize: 1024,
						PlainSize:     512,
						PlainOffset:   0,

						Redundancy: defaultTestRedundancy,
					},
				},
			}.Check(ctx, t, db)
		})
	})
}
