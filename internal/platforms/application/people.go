package application

import (
	"context"
	"errors"

	"github.com/SebastianP07/wallet-track-core-app/internal/platforms/domain"
	"github.com/SebastianP07/wallet-track-core-app/internal/shared/kernel"
)

type ListPeople struct {
	repo Repository
}

func NewListPeople(repo Repository) *ListPeople {
	return &ListPeople{repo: repo}
}

// Execute says for each person whether a payment covers the month, when given one.
func (u *ListPeople) Execute(ctx context.Context, month *kernel.YearMonth) ([]domain.PersonView, error) {
	people, err := u.repo.ListPeople(ctx)
	if err != nil {
		return nil, err
	}
	return viewsOf(ctx, u.repo, people, month)
}

type GetPerson struct {
	repo Repository
}

func NewGetPerson(repo Repository) *GetPerson {
	return &GetPerson{repo: repo}
}

func (u *GetPerson) Execute(ctx context.Context, id string) (domain.PersonView, error) {
	person, err := u.repo.GetPerson(ctx, id)
	if err != nil {
		return domain.PersonView{}, err
	}
	return viewOf(ctx, u.repo, person)
}

type CreatePerson struct {
	tx TxManager
}

func NewCreatePerson(tx TxManager) *CreatePerson {
	return &CreatePerson{tx: tx}
}

func (u *CreatePerson) Execute(ctx context.Context, person domain.NewPerson) (domain.PersonView, error) {
	var view domain.PersonView
	err := u.tx.WithinTx(ctx, func(ctx context.Context, repo Repository) error {
		if err := repo.LockPlatforms(ctx); err != nil {
			return err
		}
		created, err := repo.CreatePerson(ctx, person)
		if err != nil {
			return err
		}
		view, err = viewOf(ctx, repo, created)
		return err
	})
	return view, err
}

type UpdatePerson struct {
	tx TxManager
}

func NewUpdatePerson(tx TxManager) *UpdatePerson {
	return &UpdatePerson{tx: tx}
}

// Execute affects what is owed from now on. A frequency or a name changes no item or payment that
// already exists.
func (u *UpdatePerson) Execute(ctx context.Context, id string, changes domain.PersonChanges) (domain.PersonView, error) {
	var view domain.PersonView
	err := u.tx.WithinTx(ctx, func(ctx context.Context, repo Repository) error {
		if err := repo.LockPlatforms(ctx); err != nil {
			return err
		}
		person, err := repo.GetPerson(ctx, id)
		if err != nil {
			return err
		}
		updated, err := repo.UpdatePerson(ctx, changes.ApplyTo(person))
		if err != nil {
			return err
		}
		// Someone who stops being the owner starts owing their shares.
		if err := checkOwed(ctx, repo); err != nil {
			return err
		}
		view, err = viewOf(ctx, repo, updated)
		return err
	})
	return view, err
}

type DeletePerson struct {
	tx TxManager
}

func NewDeletePerson(tx TxManager) *DeletePerson {
	return &DeletePerson{tx: tx}
}

// Execute leaves the rest to the database: memberships go, past income items stay without the link, and
// a person with payments is refused, because those are kept as history.
func (u *DeletePerson) Execute(ctx context.Context, id string) error {
	return u.tx.WithinTx(ctx, func(ctx context.Context, repo Repository) error {
		if err := repo.LockPlatforms(ctx); err != nil {
			return err
		}
		found, err := repo.DeletePerson(ctx, id)
		if err != nil {
			return err
		}
		if !found {
			return domain.ErrPersonNotFound
		}
		// One member fewer raises everyone else's share.
		return checkOwed(ctx, repo)
	})
}

func viewOf(ctx context.Context, repo Repository, person domain.Person) (domain.PersonView, error) {
	views, err := viewsOf(ctx, repo, []domain.Person{person}, nil)
	if err != nil {
		return domain.PersonView{}, err
	}
	return views[0], nil
}

func viewsOf(ctx context.Context, repo Repository, people []domain.Person, month *kernel.YearMonth) ([]domain.PersonView, error) {
	platforms, err := repo.ListPlatforms(ctx)
	if err != nil {
		return nil, err
	}
	payments, err := repo.ListPayments(ctx)
	if err != nil {
		return nil, err
	}
	views := make([]domain.PersonView, 0, len(people))
	for _, person := range people {
		view, err := domain.NewPersonView(person, platforms, payments, month)
		if err != nil {
			return nil, err
		}
		views = append(views, view)
	}
	return views, nil
}

// checkOwed runs after a write, under the platforms lock, and refuses one that would leave someone
// owing a sum the contract cannot carry.
func checkOwed(ctx context.Context, repo Repository) error {
	people, err := repo.ListPeople(ctx)
	if err != nil {
		return err
	}
	platforms, err := repo.ListPlatforms(ctx)
	if err != nil {
		return err
	}
	for _, person := range people {
		if _, err := domain.MonthlyOwed(person, platforms); err != nil {
			if errors.Is(err, kernel.ErrMoneyOutOfRange) {
				return domain.OwedOutOfRange()
			}
			return err
		}
	}
	return nil
}
